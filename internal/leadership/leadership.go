// Package leadership реализует leader election.
package leadership

import (
	"context"
	"encoding/json"
	"time"

	"cluster-tumbler/internal/etcd"
	"cluster-tumbler/internal/keys"
	"cluster-tumbler/internal/model"

	clientv3 "go.etcd.io/etcd/client/v3"
	"go.uber.org/zap"
)

type Leadership struct {
	clusterID string
	nodeID    string
	etcd      *etcd.Client
	log       *zap.Logger
}

func New(clusterID, nodeID string, etcdClient *etcd.Client, log *zap.Logger) *Leadership {
	return &Leadership{
		clusterID: clusterID,
		nodeID:    nodeID,
		etcd:      etcdClient,
		log:       log,
	}
}

func (l *Leadership) Run(ctx context.Context) (bool, error) {
	lease, err := l.etcd.Grant(ctx, 2)
	if err != nil {
		return false, err
	}

	key := keys.Leadership(l.clusterID)

	doc := model.LeadershipDocument{
		OwnerNodeID: l.nodeID,
		LeaseID:     int64(lease.ID),
		UpdatedAt:   time.Now(),
	}

	data, _ := json.Marshal(doc)

	txn := l.etcd.Txn(ctx).
		If(clientv3.Compare(clientv3.CreateRevision(key), "=", 0)).
		Then(clientv3.OpPut(key, string(data), clientv3.WithLease(lease.ID)))

	resp, err := txn.Commit()
	if err != nil {
		return false, err
	}

	return resp.Succeeded, nil
}
