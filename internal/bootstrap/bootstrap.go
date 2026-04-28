package bootstrap

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

type Bootstrapper struct {
	cfg  *config.Config
	etcd *etcd.Client
	log  *zap.Logger
}

func New(cfg *config.Config, etcdClient *etcd.Client, log *zap.Logger) *Bootstrapper {
	return &Bootstrapper{
		cfg:  cfg,
		etcd: etcdClient,
		log:  log,
	}
}

func (b *Bootstrapper) Ensure(ctx context.Context) error {
	b.log.Debug("starting bootstrap ensure")

	for _, membership := range b.cfg.Agent.Memberships {
		if err := b.ensureMembership(ctx, membership); err != nil {
			return err
		}
	}

	b.log.Debug("bootstrap ensure completed")
	return nil
}

func (b *Bootstrapper) ensureMembership(ctx context.Context, membership config.MembershipConfig) error {
	now := time.Now().UTC()

	configDoc := model.ManagementGroupConfigDocument{
		Priority:  membership.Priority,
		UpdatedAt: now,
	}

	configData, err := json.Marshal(configDoc)
	if err != nil {
		return err
	}

	configKey := keys.ManagementGroupConfig(
		b.cfg.Cluster.ID,
		membership.ClusterGroup,
		membership.ManagementGroup,
	)

	if _, err := b.etcd.TryPutIfAbsent(ctx, configKey, configData); err != nil {
		return err
	}

	desired := model.DesiredDocument{
		State:     model.DesiredIdle,
		UpdatedAt: now,
		Details:    "bootstrap",
	}

	desiredData, err := json.Marshal(desired)
	if err != nil {
		return err
	}

	desiredKey := keys.Desired(b.cfg.Cluster.ID, membership.ClusterGroup, membership.ManagementGroup)
	if _, err := b.etcd.TryPutIfAbsent(ctx, desiredKey, desiredData); err != nil {
		return err
	}

	for _, role := range membership.Roles {
		actual := model.ActualDocument{
			State:     model.ActualIdle,
			UpdatedAt: now,
		}

		health := model.HealthDocument{
			Status:    model.HealthWarning,
			UpdatedAt: now,
			Details:   "idle",
		}

		actualData, err := json.Marshal(actual)
		if err != nil {
			return err
		}

		healthData, err := json.Marshal(health)
		if err != nil {
			return err
		}

		actualKey := keys.RoleActual(
			b.cfg.Cluster.ID,
			membership.ClusterGroup,
			membership.ManagementGroup,
			b.cfg.Agent.NodeID,
			role,
		)

		healthKey := keys.RoleHealth(
			b.cfg.Cluster.ID,
			membership.ClusterGroup,
			membership.ManagementGroup,
			b.cfg.Agent.NodeID,
			role,
		)

		if _, err := b.etcd.TryPutIfAbsent(ctx, actualKey, actualData); err != nil {
			return err
		}
		if _, err := b.etcd.TryPutIfAbsent(ctx, healthKey, healthData); err != nil {
			return err
		}
	}

	return nil
}
