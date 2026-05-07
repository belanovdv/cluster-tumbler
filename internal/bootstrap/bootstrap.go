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

	if err := b.ensureDynamicConfig(ctx); err != nil {
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

func (b *Bootstrapper) ensureDynamicConfig(ctx context.Context) error {
	key := keys.ConfigMeta(b.cfg.Cluster.ID)

	doc, exists, err := b.loadDynamicConfig(ctx, key)
	if err != nil {
		return err
	}

	changed := false
	if !exists {
		doc = model.DynamicConfigDocument{}
		changed = true
	}

	if doc.Cluster.ID == "" {
		doc.Cluster.ID = b.cfg.Cluster.ID
		changed = true
	}

	if doc.Cluster.Name == "" {
		doc.Cluster.Name = b.cfg.Cluster.Name
		changed = true
	}

	if doc.ClusterGroups == nil {
		doc.ClusterGroups = make(map[string]model.DynamicConfigNameDocument)
		changed = true
	}

	for groupID, groupCfg := range b.cfg.Cluster.Groups {
		item, ok := doc.ClusterGroups[groupID]
		if !ok {
			doc.ClusterGroups[groupID] = model.DynamicConfigNameDocument{
				ID:   groupID,
				Name: groupCfg.Name,
			}
			changed = true
			continue
		}

		if item.ID == "" {
			item.ID = groupID
			changed = true
		}

		if item.Name == "" {
			item.Name = groupCfg.Name
			changed = true
		}

		doc.ClusterGroups[groupID] = item
	}

	if doc.Roles == nil {
		doc.Roles = make(map[string]model.DynamicConfigNameDocument)
		changed = true
	}

	for roleID, roleCfg := range b.cfg.Roles {
		item, ok := doc.Roles[roleID]
		if !ok {
			doc.Roles[roleID] = model.DynamicConfigNameDocument{
				ID:   roleID,
				Name: roleCfg.Name,
			}
			changed = true
			continue
		}

		if item.ID == "" {
			item.ID = roleID
			changed = true
		}

		if item.Name == "" {
			item.Name = roleCfg.Name
			changed = true
		}

		doc.Roles[roleID] = item
	}

	if doc.Nodes == nil {
		doc.Nodes = make(map[string]model.DynamicConfigNameDocument)
		changed = true
	}

	nodeID := b.cfg.Node.NodeID
	node, ok := doc.Nodes[nodeID]
	if !ok {
		doc.Nodes[nodeID] = model.DynamicConfigNameDocument{
			ID:   nodeID,
			Name: b.cfg.Node.Name,
		}
		changed = true
	} else {
		if node.ID == "" {
			node.ID = nodeID
			changed = true
		}

		if node.Name == "" {
			node.Name = b.cfg.Node.Name
			changed = true
		}

		doc.Nodes[nodeID] = node
	}

	if !changed {
		b.log.Debug("dynamic config is already up to date", zap.String("key", key))
		return nil
	}

	doc.UpdatedAt = time.Now().UTC()

	data, err := json.Marshal(doc)
	if err != nil {
		return err
	}

	b.log.Debug("writing dynamic config", zap.String("key", key))

	return b.etcd.Put(ctx, key, data)
}

func (b *Bootstrapper) loadDynamicConfig(
	ctx context.Context,
	key string,
) (model.DynamicConfigDocument, bool, error) {
	items, _, err := b.etcd.GetPrefix(ctx, key)
	if err != nil {
		return model.DynamicConfigDocument{}, false, err
	}

	raw, ok := items[key]
	if !ok {
		return model.DynamicConfigDocument{}, false, nil
	}

	var doc model.DynamicConfigDocument
	if err := json.Unmarshal(raw, &doc); err != nil {
		return model.DynamicConfigDocument{}, false, err
	}

	return doc, true, nil
}

func (b *Bootstrapper) ensureMembership(ctx context.Context, membership config.MembershipConfig) error {
	now := time.Now().UTC()

	mgCfg := b.cfg.ManagementGroups[membership.ClusterGroup][membership.ManagementGroup]

	configDoc := model.ManagementGroupConfigDocument{
		Priority:  mgCfg.Priority,
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
