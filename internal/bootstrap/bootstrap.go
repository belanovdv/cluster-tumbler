package bootstrap

import (
	"context"
	"encoding/json"
	"time"

	"cluster-tumbler/internal/config"
	"cluster-tumbler/internal/etcd"
	"cluster-tumbler/internal/keys"
	"cluster-tumbler/internal/model"

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

	if err := b.ensureClusterConfig(ctx); err != nil {
		return err
	}

	for groupID, groupCfg := range b.cfg.Cluster.Groups {
		if err := b.ensureClusterGroup(ctx, groupID, groupCfg); err != nil {
			return err
		}
	}

	for roleID, roleCfg := range b.cfg.Roles {
		if err := b.ensureRole(ctx, roleID, roleCfg); err != nil {
			return err
		}
	}

	if err := b.ensureNode(ctx); err != nil {
		return err
	}

	for _, membership := range b.cfg.Node.Memberships {
		if err := b.ensureMembership(ctx, membership); err != nil {
			return err
		}
	}

	b.log.Debug("bootstrap ensure completed")

	return nil
}

// ensureClusterConfig seeds cluster-wide parameters to config/_meta.
// Uses TryPutIfAbsent so only the first node to start writes the value;
// subsequent changes are made by the leader via API.
func (b *Bootstrapper) ensureClusterConfig(ctx context.Context) error {
	key := keys.ConfigMeta(b.cfg.Cluster.ID)

	doc := model.ClusterConfigDocument{
		ID:                  b.cfg.Cluster.ID,
		Name:                b.cfg.Cluster.Name,
		FailoverMode:        b.cfg.Cluster.FailoverMode,
		LeaderTTL:           b.cfg.Cluster.LeaderTTL.Duration.String(),
		LeaderRenewInterval: b.cfg.Cluster.LeaderRenewInterval.Duration.String(),
		SessionTTL:          b.cfg.Cluster.SessionTTL.Duration.String(),
		UpdatedAt:           time.Now().UTC(),
	}

	data, err := json.Marshal(doc)
	if err != nil {
		return err
	}

	created, err := b.etcd.TryPutIfAbsent(ctx, key, data)
	if err != nil {
		return err
	}

	if created {
		b.log.Debug("seeded cluster config", zap.String("key", key))
	}

	return nil
}

// ensureClusterGroup seeds display config for a cluster group.
func (b *Bootstrapper) ensureClusterGroup(ctx context.Context, groupID string, groupCfg config.ClusterGroupConfig) error {
	key := keys.ConfigClusterGroupMeta(b.cfg.Cluster.ID, groupID)

	doc := model.ClusterGroupConfigDocument{
		ID:        groupID,
		Name:      groupCfg.Name,
		UpdatedAt: time.Now().UTC(),
	}

	data, err := json.Marshal(doc)
	if err != nil {
		return err
	}

	created, err := b.etcd.TryPutIfAbsent(ctx, key, data)
	if err != nil {
		return err
	}

	if created {
		b.log.Debug("seeded cluster group config", zap.String("key", key))
	}

	return nil
}

// ensureRole seeds the full role definition (actors + timeouts) to config/roles/{id}.
// Uses TryPutIfAbsent — first node wins; leader can override via API.
func (b *Bootstrapper) ensureRole(ctx context.Context, roleID string, roleCfg config.RoleConfig) error {
	key := keys.ConfigRole(b.cfg.Cluster.ID, roleID)

	actors := make(map[string][]string, len(roleCfg.Actors))
	for name, cmd := range roleCfg.Actors {
		actors[name] = []string(cmd)
	}

	doc := model.RoleConfigDocument{
		ID:     roleID,
		Name:   roleCfg.Name,
		Actors: actors,
		Timeouts: model.RoleTimeoutsDocument{
			Exec:           roleCfg.Timeouts.Exec.Duration.String(),
			Converge:       roleCfg.Timeouts.Converge.Duration.String(),
			RetryInterval:  roleCfg.Timeouts.RetryInterval.Duration.String(),
			CheckInterval:  roleCfg.Timeouts.CheckInterval.Duration.String(),
			DetailsMaxSize: roleCfg.Timeouts.DetailsMaxSize,
		},
		UpdatedAt: time.Now().UTC(),
	}

	data, err := json.Marshal(doc)
	if err != nil {
		return err
	}

	created, err := b.etcd.TryPutIfAbsent(ctx, key, data)
	if err != nil {
		return err
	}

	if created {
		b.log.Debug("seeded role config", zap.String("key", key))
	}

	return nil
}

// ensureNode writes this node's own config to config/nodes/{node_id}.
// Uses Put (not TryPutIfAbsent) so that name/membership changes in the
// local config file are reflected on restart.
func (b *Bootstrapper) ensureNode(ctx context.Context) error {
	key := keys.ConfigNode(b.cfg.Cluster.ID, b.cfg.Node.NodeID)

	memberships := make([]model.MembershipRef, len(b.cfg.Node.Memberships))
	for i, m := range b.cfg.Node.Memberships {
		memberships[i] = model.MembershipRef{
			ClusterGroup:    m.ClusterGroup,
			ManagementGroup: m.ManagementGroup,
		}
	}

	doc := model.NodeConfigDocument{
		ID:          b.cfg.Node.NodeID,
		Name:        b.cfg.Node.Name,
		Memberships: memberships,
		UpdatedAt:   time.Now().UTC(),
	}

	data, err := json.Marshal(doc)
	if err != nil {
		return err
	}

	if err := b.etcd.Put(ctx, key, data); err != nil {
		return err
	}

	b.log.Debug("wrote node config", zap.String("key", key))

	return nil
}

// ensureMembership seeds management group config and the initial desired state.
func (b *Bootstrapper) ensureMembership(ctx context.Context, membership config.MembershipConfig) error {
	now := time.Now().UTC()

	mgCfg := b.cfg.ManagementGroups[membership.ClusterGroup][membership.ManagementGroup]

	configDoc := model.ManagementGroupConfigDocument{
		Priority:  mgCfg.Priority,
		Roles:     mgCfg.Roles,
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

	created, err := b.etcd.TryPutIfAbsent(ctx, configKey, configData)
	if err != nil {
		return err
	}

	if created {
		b.log.Debug("seeded management group config", zap.String("key", configKey))
	}

	desired := model.DesiredDocument{
		State:     model.DesiredIdle,
		UpdatedAt: now,
		Details:   "bootstrap",
	}

	desiredData, err := json.Marshal(desired)
	if err != nil {
		return err
	}

	desiredKey := keys.Desired(
		b.cfg.Cluster.ID,
		membership.ClusterGroup,
		membership.ManagementGroup,
	)

	if _, err := b.etcd.TryPutIfAbsent(ctx, desiredKey, desiredData); err != nil {
		return err
	}

	// Role actual/health are runtime keys owned by the agent session lease.
	// Bootstrap must not create persistent role state, otherwise stale role state
	// would remain after an agent loss.

	return nil
}
