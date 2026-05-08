// Package api implements the HTTP API server and SSE state stream.
package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"cluster-tumbler/internal/model"
	"cluster-tumbler/internal/store"
	"cluster-tumbler/internal/web"

	"go.uber.org/zap"
)

type Putter interface {
	Put(ctx context.Context, key string, value []byte) error
}

type Server struct {
	addr      string
	clusterID string
	token     string
	store     *store.StateStore
	putter    Putter
	log       *zap.Logger
}

func New(addr string, clusterID string, token string, st *store.StateStore, putter Putter, log *zap.Logger) *Server {
	return &Server{
		addr:      addr,
		clusterID: clusterID,
		token:     token,
		store:     st,
		putter:    putter,
		log:       log,
	}
}

// requireAuth wraps a handler with Bearer token validation; passes through if token is unconfigured.
func (s *Server) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	if s.token == "" {
		return next
	}
	return func(w http.ResponseWriter, r *http.Request) {
		bearer, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || bearer != s.token {
			w.Header().Set("WWW-Authenticate", "Bearer")
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

func (s *Server) Run(ctx context.Context) error {

	mux := http.NewServeMux()

	mux.Handle("/assets/", web.AssetsHandler())
	mux.HandleFunc("/", web.Handler())
	mux.HandleFunc("/api/v1/state", s.requireAuth(s.handleState))
	mux.HandleFunc("/api/v1/stream", s.requireAuth(s.handleStream))
	mux.HandleFunc("/api/v1/commands", s.requireAuth(s.handleCommands))

	server := &http.Server{
		Addr:    s.addr,
		Handler: mux,
	}

	go func() {
		<-ctx.Done()

		shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()

		if err := server.Shutdown(shutdownCtx); err != nil {
			s.log.Warn("api server shutdown failed", zap.Error(err))
		}
	}()

	s.log.Info("starting api server", zap.String("listen", s.addr))

	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}

	return nil
}

// handleStream is the SSE entry point; sends an initial snapshot then one event per store change.
func (s *Server) handleStream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	send := func() {
		view := BuildStateView(
			s.clusterID,
			s.store.Ready(),
			s.store.Revision(),
			s.store.Snapshot(),
		)
		data, err := json.Marshal(view)
		if err != nil {
			s.log.Error("failed to marshal state for stream", zap.Error(err))
			return
		}
		fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()
	}

	send()

	for {
		select {
		case <-s.store.Notify():
			send()
		case <-r.Context().Done():
			return
		}
	}
}

// handleState returns the current cluster state as a pretty-printed JSON snapshot.
func (s *Server) handleState(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	view := BuildStateView(
		s.clusterID,
		s.store.Ready(),
		s.store.Revision(),
		s.store.Snapshot(),
	)

	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")

	if err := encoder.Encode(view); err != nil {
		s.log.Error("failed to encode state response", zap.Error(err))
	}
}

// handleCommands is the producer side of the command queue: writes a Command document to etcd and returns its ID.
// The consumer (leader queue processor) is not yet implemented.
func (s *Server) handleCommands(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Type            string             `json:"type"`
		ClusterGroup    string             `json:"cluster_group"`
		ManagementGroup string             `json:"management_group"`
		Desired         model.DesiredState `json:"desired"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	if req.Type == "" {
		req.Type = "set_desired"
	}

	cmd := model.Command{
		ID:              time.Now().UTC().Format("20060102T150405.000000000"),
		Type:            req.Type,
		ClusterGroup:    req.ClusterGroup,
		ManagementGroup: req.ManagementGroup,
		Desired:         req.Desired,
		Status:          model.CommandPending,
		CreatedAt:       time.Now().UTC(),
	}

	data, err := json.Marshal(cmd)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	key := model.CommandKey(s.clusterID, cmd.ID)

	if s.putter == nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}

	if err := s.putter.Put(r.Context(), key, data); err != nil {
		s.log.Error("failed to write command", zap.Error(err))
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	s.log.Debug("command accepted", zap.String("command_id", cmd.ID), zap.String("key", key))

	w.Header().Set("Content-Type", "application/json")

	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")

	_ = encoder.Encode(map[string]any{
		"accepted":   true,
		"command_id": cmd.ID,
		"status":     cmd.Status,
	})
}
