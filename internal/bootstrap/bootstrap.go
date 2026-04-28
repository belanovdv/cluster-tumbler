// Package bootstrap создает начальные ключи (idempotent).
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

type Bootstrap struct {
	cfg  *config.Config
	etcd *etcd.Client
	log  *zap.Logger
}

func New(cfg *config.Config, etcdClient *etcd.Client, log *zap.Logger) *Bootstrap {
	return &Bootstrap{cfg: cfg, etcd: etcdClient, log: log}
}

// Run — создает initial desired/config если ключей нет
func (b *Bootstrap) Run(ctx context.Context) error {
	for _, m := range b.cfg.Agent.Memberships {

		cfgKey := keys.ManagementGroupConfig(
			b.cfg.Cluster.ID,
			m.ClusterGroup,
			m.ManagementGroup,
		)

		doc := model.ManagementGroupConfigDocument{
			Priority:  m.ManagementGroupPriority,
			UpdatedAt: time.Now(),
		}

		data, _ := json.Marshal(doc)

		// create if not exists
		txn := b.etcd.Txn(ctx).
			If().
			Then()

		_, err := txn.Commit()
		if err != nil {
			return err
		}

		_ = b.etcd.Put(ctx, cfgKey, string(data))

		desired := model.DesiredDocument{
			State:     model.DesiredIdle,
			UpdatedAt: time.Now(),
		}

		d, _ := json.Marshal(desired)

		_ = b.etcd.Put(ctx,
			keys.Desired(b.cfg.Cluster.ID, m.ClusterGroup, m.ManagementGroup),
			string(d),
		)
	}

	return nil
}
