package roles

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"cluster-tumbler/internal/config"
	"cluster-tumbler/internal/etcd"
	"cluster-tumbler/internal/keys"
	"cluster-tumbler/internal/model"
	"cluster-tumbler/internal/store"

	"go.uber.org/zap"
)

type Manager struct {
	cfg   *config.Config
	store *store.StateStore
	etcd  *etcd.Client
	log   *zap.Logger

	workers []*Worker
}

func New(cfg *config.Config, st *store.StateStore, etcdClient *etcd.Client, log *zap.Logger) *Manager {
	m := &Manager{
		cfg:   cfg,
		store: st,
		etcd:  etcdClient,
		log:   log,
	}

	for _, membership := range cfg.Agent.Memberships {
		for _, role := range membership.Roles {
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

	store *store.StateStore
	etcd  *etcd.Client
	log   *zap.Logger

	lastDesired model.DesiredState
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
	w.log.Debug("starting mock role worker")

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if err := w.reconcile(ctx); err != nil {
				w.log.Error("role reconcile failed", zap.Error(err))
			}

		case <-ctx.Done():
			w.log.Debug("mock role worker stopped")
			return ctx.Err()
		}
	}
}

func (w *Worker) reconcile(ctx context.Context) error {
	desired, ok := w.readDesired()
	if !ok {
		return nil
	}

	if desired == w.lastDesired {
		return nil
	}

	w.log.Debug(
		"desired changed",
		zap.String("desired", string(desired)),
		zap.String("previous", string(w.lastDesired)),
	)

	w.lastDesired = desired

	go w.applyDesired(ctx, desired)

	return nil
}

func (w *Worker) readDesired() (model.DesiredState, bool) {
	key := keys.Desired(
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
		w.log.Debug("failed to decode desired", zap.String("key", key), zap.Error(err))
		return "", false
	}

	return doc.State, true
}

func (w *Worker) applyDesired(ctx context.Context, desired model.DesiredState) {
	select {
	case <-time.After(time.Second):
	case <-ctx.Done():
		return
	}

	now := time.Now().UTC()

	actualState := model.ActualIdle
	healthStatus := model.HealthWarning
	details := ""

	switch desired {
	case model.DesiredActive:
		actualState = model.ActualActive
		healthStatus = model.HealthOK

	case model.DesiredPassive:
		actualState = model.ActualPassive
		healthStatus = model.HealthOK

	case model.DesiredIdle:
		actualState = model.ActualIdle
		healthStatus = model.HealthWarning

	default:
		actualState = model.ActualFailed
		healthStatus = model.HealthFailed
		details = "unsupported desired state"
	}

	actual := model.ActualDocument{
		State:     actualState,
		UpdatedAt: now,
		Details:   details,
	}

	health := model.HealthDocument{
		Status:    healthStatus,
		UpdatedAt: now,
		Details:   details,
	}

	actualData, err := json.Marshal(actual)
	if err != nil {
		w.log.Error("failed to marshal actual", zap.Error(err))
		return
	}

	healthData, err := json.Marshal(health)
	if err != nil {
		w.log.Error("failed to marshal health", zap.Error(err))
		return
	}

	actualKey := keys.RoleActual(
		w.cfg.Cluster.ID,
		w.membership.ClusterGroup,
		w.membership.ManagementGroup,
		w.cfg.Agent.NodeID,
		w.role,
	)

	healthKey := keys.RoleHealth(
		w.cfg.Cluster.ID,
		w.membership.ClusterGroup,
		w.membership.ManagementGroup,
		w.cfg.Agent.NodeID,
		w.role,
	)

	w.log.Debug(
		"writing mock role state",
		zap.String("actual", string(actualState)),
		zap.String("health", string(healthStatus)),
	)

	if err := w.etcd.Put(ctx, actualKey, actualData); err != nil {
		w.log.Error("failed to write actual", zap.String("key", actualKey), zap.Error(err))
		return
	}

	if err := w.etcd.Put(ctx, healthKey, healthData); err != nil {
		w.log.Error("failed to write health", zap.String("key", healthKey), zap.Error(err))
		return
	}
}
