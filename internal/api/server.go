// Package api реализует HTTP API и подключает web UI.
//
// JSON state доступен на /api/v1/state.
// HTML UI доступен на /.
// Команды создаются через /api/v1/commands, но обработка command queue пока не реализована.
package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"cluster-tumbler/internal/keys"
	"cluster-tumbler/internal/model"
	"cluster-tumbler/internal/store"
	"cluster-tumbler/internal/web"

	"go.uber.org/zap"
)

// Putter описывает минимальный интерфейс записи в backend.
type Putter interface {
	Put(ctx context.Context, key string, value []byte) error
}

// Server содержит HTTP API и web UI.
type Server struct {
	addr      string
	clusterID string
	store     *store.StateStore
	putter    Putter
	log       *zap.Logger
}

// New создает HTTP server.
func New(addr string, clusterID string, st *store.StateStore, putter Putter, log *zap.Logger) *Server {
	return &Server{
		addr:      addr,
		clusterID: clusterID,
		store:     st,
		putter:    putter,
		log:       log,
	}
}

// Run запускает HTTP API/UI.
func (s *Server) Run(ctx context.Context) error {
	mux := http.NewServeMux()

	mux.Handle("/assets/", web.AssetsHandler())
	mux.HandleFunc("/", web.Handler())
	mux.HandleFunc("/api/v1/state", s.handleState)
	mux.HandleFunc("/api/v1/commands", s.handleCommands)

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

	key := keys.Command(s.clusterID, cmd.ID)

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

	_ = encoder.Encode(map[string]interface{}{
		"accepted":   true,
		"command_id": cmd.ID,
		"status":     cmd.Status,
	})
}
