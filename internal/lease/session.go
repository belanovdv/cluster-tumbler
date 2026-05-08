// Package lease manages etcd leases for node session presence and cluster leadership.
package lease

import (
	"context"
	"encoding/json"
	"time"

	"cluster-tumbler/internal/config"
	"cluster-tumbler/internal/etcd"
	"cluster-tumbler/internal/model"

	"go.uber.org/zap"
)

type SessionManager struct {
	cfg  *config.Config
	etcd *etcd.Client
	log  *zap.Logger
}

func NewSession(cfg *config.Config, etcdClient *etcd.Client, log *zap.Logger) *SessionManager {
	return &SessionManager{
		cfg:  cfg,
		etcd: etcdClient,
		log:  log,
	}
}

func (m *SessionManager) Run(ctx context.Context) error {
	m.log.Debug("starting session manager")

	ttl := int64(m.cfg.Cluster.SessionTTL.Duration.Seconds())
	if ttl <= 0 {
		ttl = 30
	}

	leaseID, err := m.etcd.GrantLease(ctx, ttl)
	if err != nil {
		return err
	}
	defer m.etcd.ClearSessionLeaseID(leaseID)

	m.etcd.SetSessionLeaseID(leaseID)

	now := time.Now().UTC()

	memberships := make([]model.MembershipDocument, 0, len(m.cfg.Node.Memberships))
	for _, membership := range m.cfg.Node.Memberships {
		mgCfg := m.cfg.ManagementGroups[membership.ClusterGroup][membership.ManagementGroup]
		memberships = append(memberships, model.MembershipDocument{
			ClusterGroup:    membership.ClusterGroup,
			ManagementGroup: membership.ManagementGroup,
			Priority:        mgCfg.Priority,
			Roles:           mgCfg.Roles,
		})
	}

	registration := model.RegistrationDocument{
		NodeID:      m.cfg.Node.NodeID,
		Memberships: memberships,
		UpdatedAt:   now,
	}

	registrationData, err := json.Marshal(registration)
	if err != nil {
		return err
	}

	registrationKey := model.Registry(m.cfg.Cluster.ID, m.cfg.Node.NodeID)

	m.log.Debug("writing global registration", zap.String("key", registrationKey))

	if err := m.etcd.Put(ctx, registrationKey, registrationData); err != nil {
		return err
	}

	sessionDoc := model.SessionDocument{
		NodeID:    m.cfg.Node.NodeID,
		UpdatedAt: now,
	}

	sessionData, err := json.Marshal(sessionDoc)
	if err != nil {
		return err
	}

	sessionKey := model.Session(m.cfg.Cluster.ID, m.cfg.Node.NodeID)

	m.log.Debug(
		"writing global session",
		zap.String("key", sessionKey),
		zap.Int64("lease_id", int64(leaseID)),
	)

	if err := m.etcd.PutWithLease(ctx, sessionKey, sessionData, leaseID); err != nil {
		return err
	}

	keepAliveCh, err := m.etcd.KeepAlive(ctx, leaseID)
	if err != nil {
		return err
	}

	for {
		select {
		case _, ok := <-keepAliveCh:
			if !ok {
				m.log.Warn("session keepalive channel closed")
				return nil
			}

		case <-ctx.Done():
			m.log.Debug("session manager stopped")
			return ctx.Err()
		}
	}
}
