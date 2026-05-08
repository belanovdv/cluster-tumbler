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

// handleCommands accepts management commands (promote, disable, reload, idle_drain) and enqueues them for the leader consumer.
func (s *Server) handleCommands(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Type            string `json:"type"`
		ClusterGroup    string `json:"cluster_group"`
		ManagementGroup string `json:"management_group"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	cmdType := model.CommandType(req.Type)
	switch cmdType {
	case model.CommandTypePromote, model.CommandTypeDisable, model.CommandTypeReload, model.CommandTypeIdleDrain:
	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": fmt.Sprintf("unknown command type %q; valid types: promote, disable, reload, idle_drain", req.Type),
		})
		return
	}

	if req.ClusterGroup == "" || req.ManagementGroup == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "cluster_group and management_group are required"})
		return
	}

	if cmdType == model.CommandTypePromote {
		if code, msg := s.validatePromote(req.ClusterGroup, req.ManagementGroup); code != 0 {
			writeJSON(w, code, map[string]string{"error": msg})
			return
		}
	}

	if cmdType == model.CommandTypeIdleDrain {
		if code, msg := s.validateIdleDrain(req.ClusterGroup, req.ManagementGroup); code != 0 {
			writeJSON(w, code, map[string]string{"error": msg})
			return
		}
	}

	cmd := model.Command{
		ID:              time.Now().UTC().Format("20060102T150405.000000000"),
		Type:            cmdType,
		ClusterGroup:    req.ClusterGroup,
		ManagementGroup: req.ManagementGroup,
		Status:          model.CommandPending,
		CreatedAt:       time.Now().UTC(),
	}

	data, err := json.Marshal(cmd)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	if err := s.putter.Put(r.Context(), model.CommandKey(s.clusterID, cmd.ID), data); err != nil {
		s.log.Error("failed to write command", zap.Error(err))
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	s.log.Info("command accepted", zap.String("id", cmd.ID), zap.String("type", string(cmd.Type)))

	writeJSON(w, http.StatusAccepted, map[string]any{
		"id":     cmd.ID,
		"type":   cmd.Type,
		"status": cmd.Status,
	})
}

// validatePromote checks topology and IDLE constraints for a promote command.
// Returns (0, "") if valid; (httpStatusCode, errorMessage) if blocked.
func (s *Server) validatePromote(clusterGroup, managementGroup string) (int, string) {
	groupPrefix := model.ClusterGroup(s.clusterID, clusterGroup)
	children := s.store.ListChildren(groupPrefix)

	if len(children) == 0 {
		return http.StatusBadRequest, fmt.Sprintf("cluster group %q not found or has no management groups", clusterGroup)
	}

	type mgInfo struct {
		priority int
		desired  model.DesiredState
		actual   model.ActualState
		hasCfg   bool
	}
	infos := make(map[string]mgInfo, len(children))
	targetFound := false

	for _, mg := range children {
		info := mgInfo{}

		if raw, ok := s.store.Get(model.ManagementGroupConfig(s.clusterID, clusterGroup, mg)); ok {
			var doc model.ManagementGroupConfigDocument
			if json.Unmarshal(raw, &doc) == nil {
				info.priority = doc.Priority
				info.hasCfg = true
			}
		}

		if raw, ok := s.store.Get(model.Desired(s.clusterID, clusterGroup, mg)); ok {
			var doc model.DesiredDocument
			if json.Unmarshal(raw, &doc) == nil {
				info.desired = doc.State
			}
		}

		if raw, ok := s.store.Get(model.Actual(s.clusterID, clusterGroup, mg)); ok {
			var doc model.ActualDocument
			if json.Unmarshal(raw, &doc) == nil {
				info.actual = doc.State
			}
		}

		infos[mg] = info
		if mg == managementGroup {
			targetFound = true
		}
	}

	if !targetFound {
		return http.StatusBadRequest, fmt.Sprintf("management group %q not found in cluster group %q", managementGroup, clusterGroup)
	}

	// Detect active-active: all groups share the same priority
	firstPri, firstSet, allSame := 0, false, true
	for _, info := range infos {
		if !info.hasCfg {
			continue
		}
		if !firstSet {
			firstPri = info.priority
			firstSet = true
			continue
		}
		if info.priority != firstPri {
			allSame = false
			break
		}
	}

	if firstSet && allSame {
		return http.StatusBadRequest, "promote is not applicable: all management groups have equal priority (active-active topology)"
	}

	// Active-passive: block only if a sibling with desired=idle has services that may still be running.
	// actual=idle/passive/failed means services are confirmed down; allow promote in those cases.
	for mg, info := range infos {
		if mg == managementGroup {
			continue
		}
		if info.desired == model.DesiredIdle &&
			(info.actual == model.ActualActive || info.actual == model.ActualStarting) {
			return http.StatusConflict, fmt.Sprintf(
				"cannot promote %q: management group %q has desired=idle but actual=%s (services may still be active)",
				managementGroup, mg, info.actual,
			)
		}
	}

	return 0, ""
}

// validateIdleDrain checks that the target management group has desired=idle.
// Returns (0, "") if valid; (httpStatusCode, errorMessage) if blocked.
func (s *Server) validateIdleDrain(clusterGroup, managementGroup string) (int, string) {
	raw, ok := s.store.Get(model.Desired(s.clusterID, clusterGroup, managementGroup))
	if !ok {
		return http.StatusBadRequest, fmt.Sprintf("management group %q not found in cluster group %q", managementGroup, clusterGroup)
	}
	var doc model.DesiredDocument
	if err := json.Unmarshal(raw, &doc); err != nil {
		return http.StatusInternalServerError, "failed to read desired state"
	}
	if doc.State != model.DesiredIdle {
		return http.StatusConflict, fmt.Sprintf("idle_drain requires desired=idle, got desired=%s", doc.State)
	}
	return 0, ""
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
