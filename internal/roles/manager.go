// Package roles runs per-role execution workers and actor-based convergence.
package roles

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"cluster-tumbler/internal/config"
	"cluster-tumbler/internal/etcd"
	"cluster-tumbler/internal/model"
	"cluster-tumbler/internal/store"

	"go.uber.org/zap"
)

type Manager struct {
	cfg     *config.Config
	store   *store.StateStore
	etcd    *etcd.Client
	log     *zap.Logger
	workers []*Worker
}

func New(
	cfg *config.Config,
	st *store.StateStore,
	etcdClient *etcd.Client,
	log *zap.Logger,
) *Manager {
	m := &Manager{
		cfg:   cfg,
		store: st,
		etcd:  etcdClient,
		log:   log,
	}

	for _, membership := range cfg.Node.Memberships {
		mgCfg := cfg.ManagementGroups[membership.ClusterGroup][membership.ManagementGroup]
		for _, role := range mgCfg.Roles {
			m.workers = append(m.workers, NewWorker(
				cfg,
				membership,
				role,
				st,
				etcdClient,
				log.With(
					zap.String("cluster_group", membership.ClusterGroup),
					zap.String("management_group", membership.ManagementGroup),
					zap.String("role", role),
				),
			))
		}
	}

	return m
}

func (m *Manager) Run(ctx context.Context) error {
	m.log.Debug("starting role manager", zap.Int("workers", len(m.workers)))

	var wg sync.WaitGroup

	for _, worker := range m.workers {
		wg.Add(1)

		go func(w *Worker) {
			defer wg.Done()

			if err := w.Run(ctx); err != nil && err != context.Canceled {
				m.log.Error("role worker failed", zap.Error(err))
			}
		}(worker)
	}

	<-ctx.Done()

	wg.Wait()

	m.log.Debug("role manager stopped")

	return ctx.Err()
}

type Worker struct {
	cfg        *config.Config
	membership config.MembershipConfig
	role       string
	store      *store.StateStore
	etcd       *etcd.Client
	log        *zap.Logger

	lastDesired model.DesiredState
	lastActual  model.ActualState
	lastCheckAt time.Time

	mu            sync.Mutex
	desiredCancel context.CancelFunc
}

func NewWorker(
	cfg *config.Config,
	membership config.MembershipConfig,
	role string,
	st *store.StateStore,
	etcdClient *etcd.Client,
	log *zap.Logger,
) *Worker {
	return &Worker{
		cfg:         cfg,
		membership:  membership,
		role:        role,
		store:       st,
		etcd:        etcdClient,
		log:         log,
		lastDesired: "",
	}
}

func (w *Worker) Run(ctx context.Context) error {
	w.log.Debug("starting role worker")

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if err := w.reconcile(ctx); err != nil {
				w.log.Error("role reconcile failed", zap.Error(err))
			}

		case <-ctx.Done():
			w.cancelCurrentDesired()
			w.log.Debug("role worker stopped")
			return ctx.Err()
		}
	}
}

func (w *Worker) readDesired() (model.DesiredState, bool) {
	key := model.Desired(
		w.cfg.Cluster.ID,
		w.membership.ClusterGroup,
		w.membership.ManagementGroup,
	)

	raw, ok := w.store.Get(key)
	if !ok {
		return "", false
	}

	var doc model.DesiredDocument
	if err := json.Unmarshal(raw, &doc); err != nil {
		w.log.Warn(
			"failed to decode desired",
			zap.String("key", key),
			zap.Error(err),
		)
		return "", false
	}

	return doc.State, true
}

// reconcile decides whether to run desired execution: on desired change or when the check interval elapses.
func (w *Worker) reconcile(ctx context.Context) error {
	desired, ok := w.readDesired()
	if !ok {
		return nil
	}

	now := time.Now()

	roleCfg, ok := w.cfg.Roles[w.role]
	if !ok {
		return nil
	}

	checkInterval := roleCfg.Timeouts.CheckInterval.Duration
	if checkInterval <= 0 {
		checkInterval = 5 * time.Second
	}

	if desired != w.lastDesired {
		w.log.Debug(
			"desired changed",
			zap.String("desired", string(desired)),
			zap.String("previous", string(w.lastDesired)),
		)

		w.lastDesired = desired
		w.lastCheckAt = now

		w.startDesiredExecution(ctx, desired)
		return nil
	}

	if w.lastCheckAt.IsZero() || now.Sub(w.lastCheckAt) >= checkInterval {
		w.lastCheckAt = now
		w.startDesiredExecution(ctx, desired)
	}

	return nil
}

// startDesiredExecution cancels any in-flight execution and starts a new goroutine for the given desired state.
func (w *Worker) startDesiredExecution(parent context.Context, desired model.DesiredState) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.desiredCancel != nil {
		w.desiredCancel()
		w.desiredCancel = nil
	}

	ctx, cancel := context.WithCancel(parent)
	w.desiredCancel = cancel

	go func() {
		defer cancel()

		w.applyDesired(ctx, desired)
	}()
}

func (w *Worker) cancelCurrentDesired() {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.desiredCancel != nil {
		w.desiredCancel()
		w.desiredCancel = nil
	}
}

// applyDesired waits for a session lease, builds the RoleExecutor, runs Reconcile, and writes the resulting status.
func (w *Worker) applyDesired(ctx context.Context, desired model.DesiredState) {
	if err := w.waitForSessionLease(ctx); err != nil {
		w.log.Debug("session lease is not ready, skip role execution", zap.Error(err))
		return
	}

	roleCfg, ok := w.cfg.Roles[w.role]
	if !ok {
		w.writeStatus(ctx, RoleStatus{
			State:  string(model.ActualFailed),
			Health: string(model.HealthFailed),
			Details: map[string]any{
				"error": fmt.Sprintf("role %q is not defined in config", w.role),
			},
		})
		return
	}

	executor := &RoleExecutor{
		Runner: &ExecActorRunner{
			Timeout:        roleCfg.Timeouts.Exec.Duration,
			DetailsMaxSize: roleCfg.Timeouts.DetailsMaxSize,
		},
		Actors:        toExecutorActors(roleCfg.Actors, w.cfg),
		Converge:      roleCfg.Timeouts.Converge.Duration,
		RetryInterval: roleCfg.Timeouts.RetryInterval.Duration,
	}

	onTransition := func(status RoleStatus) {
		w.writeStatus(ctx, status)
	}

	status := executor.Reconcile(ctx, RoleRequest{
		ClusterGroup:    w.membership.ClusterGroup,
		ManagementGroup: w.membership.ManagementGroup,
		NodeID:          w.cfg.Node.NodeID,
		Role:            w.role,
		Desired:         string(desired),
	}, onTransition)

	if model.ActualState(status.State) != w.lastActual {
		w.log.Debug(
			"role actual state changed",
			zap.String("desired", string(desired)),
			zap.String("actual", status.State),
			zap.String("health", status.Health),
			zap.Any("details", status.Details),
		)
		w.lastActual = model.ActualState(status.State)
	}

	w.writeStatus(ctx, status)
}

// waitForSessionLease polls until the etcd session lease is available or ctx is cancelled.
func (w *Worker) waitForSessionLease(ctx context.Context) error {
	if w.etcd.SessionLeaseID() != 0 {
		return nil
	}

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if w.etcd.SessionLeaseID() != 0 {
				return nil
			}

		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// writeStatus serialises and writes actual and health documents to etcd under the session lease, skipping unchanged values.
func (w *Worker) writeStatus(ctx context.Context, status RoleStatus) {
	leaseID := w.etcd.SessionLeaseID()
	if leaseID == 0 {
		w.log.Warn("cannot write role state without session lease")
		return
	}

	now := time.Now().UTC()

	details := detailsToString(status.Details)

	actual := model.ActualDocument{
		State:     toActualState(status.State),
		UpdatedAt: now,
		Details:   details,
	}

	health := model.HealthDocument{
		Status:    toHealthStatus(status.Health),
		UpdatedAt: now,
		Details:   details,
	}

	actualKey := model.RoleActual(
		w.cfg.Cluster.ID,
		w.membership.ClusterGroup,
		w.membership.ManagementGroup,
		w.cfg.Node.NodeID,
		w.role,
	)

	healthKey := model.RoleHealth(
		w.cfg.Cluster.ID,
		w.membership.ClusterGroup,
		w.membership.ManagementGroup,
		w.cfg.Node.NodeID,
		w.role,
	)

	actualChanged := w.hasActualChanged(actualKey, actual.State)
	healthChanged := w.hasHealthChanged(healthKey, health.Status)

	if !actualChanged && !healthChanged {
		return
	}

	w.log.Debug(
		"writing role state with session lease",
		zap.String("actual", string(actual.State)),
		zap.String("health", string(health.Status)),
		zap.String("details", details),
		zap.Int64("lease_id", int64(leaseID)),
	)

	if actualChanged {
		actualData, err := json.Marshal(actual)
		if err != nil {
			w.log.Error("failed to marshal actual", zap.Error(err))
			return
		}

		if err := w.etcd.PutWithLease(ctx, actualKey, actualData, leaseID); err != nil {
			w.log.Error(
				"failed to write actual",
				zap.String("key", actualKey),
				zap.Error(err),
			)
			return
		}
	}

	if healthChanged {
		healthData, err := json.Marshal(health)
		if err != nil {
			w.log.Error("failed to marshal health", zap.Error(err))
			return
		}

		if err := w.etcd.PutWithLease(ctx, healthKey, healthData, leaseID); err != nil {
			w.log.Error(
				"failed to write health",
				zap.String("key", healthKey),
				zap.Error(err),
			)
			return
		}
	}
}

func (w *Worker) hasActualChanged(key string, state model.ActualState) bool {
	raw, ok := w.store.Get(key)
	if !ok {
		return true
	}

	var current model.ActualDocument
	if err := json.Unmarshal(raw, &current); err != nil {
		return true
	}

	return current.State != state
}

func (w *Worker) hasHealthChanged(key string, status model.HealthStatus) bool {
	raw, ok := w.store.Get(key)
	if !ok {
		return true
	}

	var current model.HealthDocument
	if err := json.Unmarshal(raw, &current); err != nil {
		return true
	}

	return current.Status != status
}

// toExecutorActors converts config.RoleActors to the executor map, resolving the executable path for each actor.
func toExecutorActors(src config.RoleActors, cfg *config.Config) map[ActorName][]string {
	out := make(map[ActorName][]string, len(src))

	for name, command := range src {
		cmd := []string(command)
		if len(cmd) > 0 {
			cmd[0] = cfg.ResolveActorPath(cmd[0])
		}
		out[ActorName(name)] = cmd
	}

	return out
}

func toActualState(state string) model.ActualState {
	switch state {
	case string(model.ActualActive):
		return model.ActualActive
	case string(model.ActualPassive):
		return model.ActualPassive
	case string(model.ActualIdle):
		return model.ActualIdle
	case string(model.ActualStarting):
		return model.ActualStarting
	case string(model.ActualStopping):
		return model.ActualStopping
	case string(model.ActualFailed):
		return model.ActualFailed
	default:
		return model.ActualFailed
	}
}

func toHealthStatus(status string) model.HealthStatus {
	switch status {
	case string(model.HealthOK):
		return model.HealthOK
	case string(model.HealthWarning):
		return model.HealthWarning
	case string(model.HealthFailed):
		return model.HealthFailed
	default:
		return model.HealthFailed
	}
}

func detailsToString(details map[string]any) string {
	if len(details) == 0 {
		return ""
	}

	data, err := json.Marshal(details)
	if err != nil {
		return fmt.Sprintf(`{"error":"failed to marshal details","marshal_error":%q}`, err.Error())
	}

	return string(data)
}
