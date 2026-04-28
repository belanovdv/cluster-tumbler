// Package session — TTL heartbeat агента.
package session

import (
	"context"
	"encoding/json"
	"time"

	"cluster-tumbler/internal/etcd"
	"cluster-tumbler/internal/keys"
	"cluster-tumbler/internal/model"

	"go.uber.org/zap"
)

type Session struct {
	clusterID string
	nodeID    string
	etcd      *etcd.Client
	log       *zap.Logger
}

func New(clusterID, nodeID string, etcdClient *etcd.Client, log *zap.Logger) *Session {
	return &Session{
		clusterID: clusterID,
		nodeID:    nodeID,
		etcd:      etcdClient,
		log:       log,
	}
}

func (s *Session) Run(ctx context.Context) error {
	lease, err := s.etcd.Grant(ctx, 2)
	if err != nil {
		return err
	}

	ch, err := s.etcd.KeepAlive(ctx, lease.ID)
	if err != nil {
		return err
	}

	go func() {
		for range ch {
		}
	}()

	doc := model.SessionDocument{
		NodeID:    s.nodeID,
		UpdatedAt: time.Now(),
	}

	data, _ := json.Marshal(doc)

	return s.etcd.Put(ctx,
		keys.Session(s.clusterID, s.nodeID),
		string(data),
	)
}
