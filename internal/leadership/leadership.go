package leadership

import (
	"context"
	"encoding/json"
	"time"

	"cluster-agent/internal/config"
	"cluster-agent/internal/etcd"
	"cluster-agent/internal/keys"
	"cluster-agent/internal/model"
	"go.uber.org/zap"
)

type Event struct {
	Kind string
}

type Manager struct {
	cfg    *config.Config
	etcd   *etcd.Client
	log    *zap.Logger
	events chan Event
}

func New(cfg *config.Config, etcdClient *etcd.Client, log *zap.Logger) *Manager {
	return &Manager{
		cfg:    cfg,
		etcd:   etcdClient,
		log:    log,
		events: make(chan Event, 8),
	}
}

func (m *Manager) Events() <-chan Event {
	return m.events
}

func (m *Manager) Run(ctx context.Context) error {
	m.log.Debug("starting leadership manager")

	for {
		if err := m.tryLeadership(ctx); err != nil {
			m.log.Warn("leadership attempt failed", zap.Error(err))
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(m.cfg.Cluster.LeaderRenewInterval.Duration):
		}
	}
}

func (m *Manager) tryLeadership(ctx context.Context) error {
	ttl := int64(m.cfg.Cluster.LeaderTTL.Duration.Seconds())
	if ttl <= 0 {
		ttl = 2
	}

	leaseID, err := m.etcd.GrantLease(ctx, ttl)
	if err != nil {
		return err
	}

	doc := model.LeadershipDocument{
		OwnerNodeID: m.cfg.Agent.NodeID,
		LeaseID:     int64(leaseID),
		UpdatedAt:   time.Now().UTC(),
	}

	data, err := json.Marshal(doc)
	if err != nil {
		return err
	}

	key := keys.Leadership(m.cfg.Cluster.ID)

	acquired, err := m.etcd.TryAcquireLeaseKey(ctx, key, data, leaseID)
	if err != nil {
		return err
	}

	if !acquired {
		// m.log.Debug("leadership is held by another agent", zap.String("key", key))
		return nil
	}

	m.log.Info("leadership acquired", zap.String("key", key), zap.Int64("lease_id", int64(leaseID)))
	m.emit(Event{Kind: "acquired"})

	keepAliveCh, err := m.etcd.KeepAlive(ctx, leaseID)
	if err != nil {
		return err
	}

	for {
		select {
		case _, ok := <-keepAliveCh:
			if !ok {
				m.log.Warn("leadership keepalive channel closed")
				m.emit(Event{Kind: "lost"})
				return nil
			}

		case <-ctx.Done():
			m.log.Debug("leadership manager stopped")
			m.emit(Event{Kind: "lost"})
			return ctx.Err()
		}
	}
}

func (m *Manager) emit(event Event) {
	select {
	case m.events <- event:
	default:
		m.log.Warn("leadership event dropped", zap.String("kind", event.Kind))
	}
}
