package cfgwatch

import (
	"context"
	"encoding/json"
	"strings"

	"cluster-tumbler/internal/config"
	"cluster-tumbler/internal/etcd"
	"cluster-tumbler/internal/keys"
	"cluster-tumbler/internal/model"
	"cluster-tumbler/internal/store"

	"go.uber.org/zap"
)

type Watcher struct {
	cfg  *config.Config
	etcd *etcd.Client
	log  *zap.Logger
}

func New(cfg *config.Config, etcdClient *etcd.Client, log *zap.Logger) *Watcher {
	return &Watcher{cfg: cfg, etcd: etcdClient, log: log}
}

func (w *Watcher) Run(ctx context.Context) error {
	w.log.Debug("starting config watcher")

	prefix := keys.ConfigRoot(w.cfg.Cluster.ID)
	events := w.etcd.WatchPrefix(ctx, prefix, 0)

	for {
		select {
		case event, ok := <-events:
			if !ok {
				w.log.Warn("config watch channel closed")
				return nil
			}

			if event.Type == store.EventPut {
				w.onConfigChanged(ctx, event.Key)
			}

		case <-ctx.Done():
			w.log.Debug("config watcher stopped")
			return ctx.Err()
		}
	}
}

func (w *Watcher) onConfigChanged(ctx context.Context, changedKey string) {
	snap, err := w.loadSnapshot(ctx)
	if err != nil {
		w.log.Warn("failed to load config snapshot after change",
			zap.String("key", changedKey),
			zap.Error(err),
		)
		return
	}

	_ = config.Merge(w.cfg, snap)

	w.log.Info("cluster config changed in etcd (not applied to agents)",
		zap.String("changed_key", changedKey),
	)
}

func (w *Watcher) loadSnapshot(ctx context.Context) (*config.EtcdSnapshot, error) {
	prefix := keys.ConfigRoot(w.cfg.Cluster.ID)

	items, _, err := w.etcd.GetPrefix(ctx, prefix)
	if err != nil {
		return nil, err
	}

	snap := &config.EtcdSnapshot{
		ClusterGroups:    make(map[string]*model.ClusterGroupConfigDocument),
		Roles:            make(map[string]*model.RoleConfigDocument),
		ManagementGroups: make(map[string]map[string]*model.ManagementGroupConfigDocument),
	}

	metaKey := keys.ConfigMeta(w.cfg.Cluster.ID)
	nodesRoot := keys.ConfigNodeRoot(w.cfg.Cluster.ID) + "/"
	rolesRoot := keys.ConfigRoleRoot(w.cfg.Cluster.ID) + "/"
	groupsRoot := keys.ConfigClusterGroupRoot(w.cfg.Cluster.ID) + "/"
	ownNodeKey := keys.ConfigNode(w.cfg.Cluster.ID, w.cfg.Node.NodeID)

	for k, raw := range items {
		switch {
		case k == metaKey:
			var doc model.ClusterConfigDocument
			if err := json.Unmarshal(raw, &doc); err == nil {
				snap.Cluster = &doc
			}

		case k == ownNodeKey:
			var doc model.NodeConfigDocument
			if err := json.Unmarshal(raw, &doc); err == nil {
				snap.Node = &doc
			}

		case strings.HasPrefix(k, nodesRoot):
			// other nodes — not needed for this node's effective config

		case strings.HasPrefix(k, rolesRoot):
			var doc model.RoleConfigDocument
			if err := json.Unmarshal(raw, &doc); err == nil && doc.ID != "" {
				snap.Roles[doc.ID] = &doc
			}

		case strings.HasPrefix(k, groupsRoot):
			// config/cluster_groups/{cg}/_meta  → ClusterGroupConfigDocument
			// config/cluster_groups/{cg}/{mg}    → ManagementGroupConfigDocument
			rel := strings.TrimPrefix(k, groupsRoot)
			parts := strings.SplitN(rel, "/", 2)
			if len(parts) != 2 {
				break
			}
			cg, sub := parts[0], parts[1]
			if sub == "_meta" {
				var doc model.ClusterGroupConfigDocument
				if err := json.Unmarshal(raw, &doc); err == nil && doc.ID != "" {
					snap.ClusterGroups[cg] = &doc
				}
			} else {
				var doc model.ManagementGroupConfigDocument
				if err := json.Unmarshal(raw, &doc); err == nil {
					if snap.ManagementGroups[cg] == nil {
						snap.ManagementGroups[cg] = make(map[string]*model.ManagementGroupConfigDocument)
					}
					snap.ManagementGroups[cg][sub] = &doc
				}
			}
		}
	}

	return snap, nil
}
