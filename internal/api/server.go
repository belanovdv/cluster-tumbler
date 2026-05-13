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

// handleCommands accepts management commands and enqueues them for the leader consumer.
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
	case model.CommandTypePromote, model.CommandTypeDemote, model.CommandTypeDisable, model.CommandTypeEnable, model.CommandTypeForcePassive:
	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": fmt.Sprintf("unknown command type %q; valid types: promote, demote, disable, enable, force_passive", req.Type),
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

	if cmdType == model.CommandTypeDemote {
		if code, msg := s.validateDemote(req.ClusterGroup, req.ManagementGroup); code != 0 {
			writeJSON(w, code, map[string]string{"error": msg})
			return
		}
	}

	if cmdType == model.CommandTypeEnable {
		if code, msg := s.validateEnable(req.ClusterGroup, req.ManagementGroup); code != 0 {
			writeJSON(w, code, map[string]string{"error": msg})
			return
		}
	}

	if cmdType == model.CommandTypeForcePassive {
		if code, msg := s.validateForcePassive(req.ClusterGroup, req.ManagementGroup); code != 0 {
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

// validatePromote checks that the promote command can safely proceed.
// Returns (0, "") if valid; (httpStatusCode, errorMessage) if blocked.
func (s *Server) validatePromote(clusterGroup, managementGroup string) (int, string) {
	groupPrefix := model.ClusterGroup(s.clusterID, clusterGroup)
	children := s.store.ListChildren(groupPrefix)

	if len(children) == 0 {
		return http.StatusBadRequest, fmt.Sprintf("cluster group %q not found or has no management groups", clusterGroup)
	}

	type mgInfo struct {
		managed bool
		actual  model.ActualState
	}
	infos := make(map[string]mgInfo, len(children))
	targetFound := false

	for _, mg := range children {
		info := mgInfo{}

		if raw, ok := s.store.Get(model.Desired(s.clusterID, clusterGroup, mg)); ok {
			var doc model.DesiredDocument
			if json.Unmarshal(raw, &doc) == nil {
				info.managed = doc.Managed
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

	// Block promote only if a sibling with managed=false is actively running —
	// the controller cannot drain it, so promoting would risk a split-brain.
	// Siblings with managed=true and actual=active are handled by the controller's
	// two-phase switchover after the priority swap is committed.
	for mg, info := range infos {
		if mg == managementGroup {
			continue
		}
		if !info.managed && (info.actual == model.ActualActive || info.actual == model.ActualStarting) {
			return http.StatusConflict, fmt.Sprintf(
				"cannot promote %q: management group %q is unmanaged (managed=false) with actual=%s — stop it manually or run force_passive first",
				managementGroup, mg, info.actual,
			)
		}
	}

	return 0, ""
}

// validateDemote checks cluster stability and candidate availability for a demote command.
// For desired=passive groups, no further validation is needed (convergence reset).
// For desired=active groups, blocks if any group is transitioning (starting/stopping) or
// if no passive managed group with health=ok is available as replacement.
// Returns (0, "") if valid; (httpStatusCode, errorMessage) if blocked.
func (s *Server) validateDemote(clusterGroup, managementGroup string) (int, string) {
	groupPrefix := model.ClusterGroup(s.clusterID, clusterGroup)
	children := s.store.ListChildren(groupPrefix)

	if len(children) == 0 {
		return http.StatusBadRequest, fmt.Sprintf("cluster group %q not found or has no management groups", clusterGroup)
	}

	targetFound := false
	var targetDesired model.DesiredState
	for _, mg := range children {
		if mg == managementGroup {
			targetFound = true
			if raw, ok := s.store.Get(model.Desired(s.clusterID, clusterGroup, mg)); ok {
				var doc model.DesiredDocument
				if json.Unmarshal(raw, &doc) == nil {
					targetDesired = doc.State
				}
			}
			break
		}
	}

	if !targetFound {
		return http.StatusBadRequest, fmt.Sprintf("management group %q not found in cluster group %q", managementGroup, clusterGroup)
	}

	if targetDesired == model.DesiredPassive {
		return 0, ""
	}

	hasCandidate := false
	for _, mg := range children {
		var actual model.ActualState
		if raw, ok := s.store.Get(model.Actual(s.clusterID, clusterGroup, mg)); ok {
			var act model.ActualDocument
			if json.Unmarshal(raw, &act) == nil {
				actual = act.State
			}
		}

		if actual == model.ActualStarting || actual == model.ActualStopping {
			return http.StatusConflict, fmt.Sprintf(
				"cannot demote %q: management group %q has actual=%s (cluster is transitioning)",
				managementGroup, mg, actual,
			)
		}

		if mg == managementGroup {
			continue
		}

		desRaw, ok := s.store.Get(model.Desired(s.clusterID, clusterGroup, mg))
		if !ok || actual != model.ActualPassive {
			continue
		}
		var des model.DesiredDocument
		if json.Unmarshal(desRaw, &des) != nil || !des.Managed {
			continue
		}
		healthRaw, ok := s.store.Get(model.Health(s.clusterID, clusterGroup, mg))
		if !ok {
			continue
		}
		var h model.HealthDocument
		if json.Unmarshal(healthRaw, &h) != nil || h.Status != model.HealthOK {
			continue
		}
		hasCandidate = true
	}

	if !hasCandidate {
		return http.StatusConflict, fmt.Sprintf(
			"cannot demote %q: no available passive managed group with health=ok in cluster group %q",
			managementGroup, clusterGroup,
		)
	}

	return 0, ""
}

// validateEnable checks that the target group has managed=false.
// Returns (0, "") if valid; (httpStatusCode, errorMessage) if blocked.
func (s *Server) validateEnable(clusterGroup, managementGroup string) (int, string) {
	raw, ok := s.store.Get(model.Desired(s.clusterID, clusterGroup, managementGroup))
	if !ok {
		return http.StatusBadRequest, fmt.Sprintf("management group %q not found in cluster group %q", managementGroup, clusterGroup)
	}
	var doc model.DesiredDocument
	if err := json.Unmarshal(raw, &doc); err != nil {
		return http.StatusInternalServerError, "failed to read desired state"
	}
	if doc.Managed {
		return http.StatusConflict, fmt.Sprintf("group %q is already under normal management (managed=true)", managementGroup)
	}
	return 0, ""
}

// validateForcePassive checks that the target group has managed=false and desired=active.
// Returns (0, "") if valid; (httpStatusCode, errorMessage) if blocked.
func (s *Server) validateForcePassive(clusterGroup, managementGroup string) (int, string) {
	raw, ok := s.store.Get(model.Desired(s.clusterID, clusterGroup, managementGroup))
	if !ok {
		return http.StatusBadRequest, fmt.Sprintf("management group %q not found in cluster group %q", managementGroup, clusterGroup)
	}
	var doc model.DesiredDocument
	if err := json.Unmarshal(raw, &doc); err != nil {
		return http.StatusInternalServerError, "failed to read desired state"
	}
	if doc.Managed {
		return http.StatusConflict, fmt.Sprintf("force_passive requires managed=false, group %q is under normal management", managementGroup)
	}
	if doc.State != model.DesiredActive {
		return http.StatusConflict, fmt.Sprintf("force_passive requires desired=active, got desired=%s", doc.State)
	}
	return 0, ""
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
