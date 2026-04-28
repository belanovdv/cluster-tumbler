package etcd

import (
	"context"
	"time"

	"cluster-tumbler/internal/store"
	clientv3 "go.etcd.io/etcd/client/v3"
	"go.uber.org/zap"
)

type Client struct {
	cli *clientv3.Client
	log *zap.Logger
}

func New(endpoints []string, dialTimeout time.Duration, log *zap.Logger) (*Client, error) {
	log.Debug(
		"initializing etcd client",
		zap.Strings("endpoints", endpoints),
		zap.Duration("dial_timeout", dialTimeout),
	)

	cli, err := clientv3.New(clientv3.Config{
		Endpoints:   endpoints,
		DialTimeout: dialTimeout,
	})
	if err != nil {
		return nil, err
	}

	return &Client{
		cli: cli,
		log: log,
	}, nil
}

func (c *Client) Close() error {
	c.log.Debug("closing etcd client")
	return c.cli.Close()
}

func (c *Client) GetPrefix(ctx context.Context, prefix string) (map[string][]byte, int64, error) {
	c.log.Debug("loading etcd prefix", zap.String("prefix", prefix))

	resp, err := c.cli.Get(ctx, prefix, clientv3.WithPrefix())
	if err != nil {
		return nil, 0, err
	}

	items := make(map[string][]byte, len(resp.Kvs))
	for _, kv := range resp.Kvs {
		items[string(kv.Key)] = append([]byte(nil), kv.Value...)
	}

	c.log.Debug(
		"loaded etcd prefix",
		zap.String("prefix", prefix),
		zap.Int("items", len(items)),
		zap.Int64("revision", resp.Header.Revision),
	)

	return items, resp.Header.Revision, nil
}

func (c *Client) WatchPrefix(ctx context.Context, prefix string, fromRevision int64) <-chan store.Event {
	out := make(chan store.Event, 128)

	c.log.Debug(
		"starting etcd watch",
		zap.String("prefix", prefix),
		zap.Int64("from_revision", fromRevision),
	)

	go func() {
		defer close(out)

		opts := []clientv3.OpOption{
			clientv3.WithPrefix(),
		}
		if fromRevision > 0 {
			opts = append(opts, clientv3.WithRev(fromRevision))
		}

		watchCh := c.cli.Watch(ctx, prefix, opts...)

		for resp := range watchCh {
			if err := resp.Err(); err != nil {
				c.log.Error("etcd watch error", zap.Error(err))
				return
			}

			for _, ev := range resp.Events {
				eventType := store.EventPut
				if ev.Type == clientv3.EventTypeDelete {
					eventType = store.EventDelete
				}

				event := store.Event{
					Type:     eventType,
					Key:      string(ev.Kv.Key),
					Revision: ev.Kv.ModRevision,
				}
				if ev.Kv.Value != nil {
					event.Value = append([]byte(nil), ev.Kv.Value...)
				}

				c.log.Debug(
					"etcd watch event",
					zap.String("type", string(event.Type)),
					zap.String("key", event.Key),
					zap.Int64("revision", event.Revision),
				)

				select {
				case out <- event:
				case <-ctx.Done():
					return
				}
			}
		}
	}()

	return out
}

func (c *Client) Put(ctx context.Context, key string, value []byte, opts ...clientv3.OpOption) error {
	c.log.Debug("etcd put", zap.String("key", key))
	_, err := c.cli.Put(ctx, key, string(value), opts...)
	return err
}

func (c *Client) PutWithLease(ctx context.Context, key string, value []byte, leaseID clientv3.LeaseID) error {
	return c.Put(ctx, key, value, clientv3.WithLease(leaseID))
}

func (c *Client) Delete(ctx context.Context, key string) error {
	c.log.Debug("etcd delete", zap.String("key", key))
	_, err := c.cli.Delete(ctx, key)
	return err
}

func (c *Client) GrantLease(ctx context.Context, ttlSeconds int64) (clientv3.LeaseID, error) {
	// c.log.Debug("granting etcd lease", zap.Int64("ttl_seconds", ttlSeconds))

	resp, err := c.cli.Grant(ctx, ttlSeconds)
	if err != nil {
		return 0, err
	}

	// c.log.Debug("etcd lease granted", zap.Int64("lease_id", int64(resp.ID)))
	return resp.ID, nil
}

func (c *Client) KeepAlive(ctx context.Context, leaseID clientv3.LeaseID) (<-chan *clientv3.LeaseKeepAliveResponse, error) {
	c.log.Debug("starting lease keepalive", zap.Int64("lease_id", int64(leaseID)))
	return c.cli.KeepAlive(ctx, leaseID)
}

func (c *Client) TryPutIfAbsent(ctx context.Context, key string, value []byte) (bool, error) {
	c.log.Debug("etcd put-if-absent", zap.String("key", key))

	txnResp, err := c.cli.Txn(ctx).
		If(clientv3.Compare(clientv3.CreateRevision(key), "=", 0)).
		Then(clientv3.OpPut(key, string(value))).
		Commit()
	if err != nil {
		return false, err
	}

	c.log.Debug(
		"etcd put-if-absent result",
		zap.String("key", key),
		zap.Bool("created", txnResp.Succeeded),
	)

	return txnResp.Succeeded, nil
}

func (c *Client) TryAcquireLeaseKey(ctx context.Context, key string, value []byte, leaseID clientv3.LeaseID) (bool, error) {
	// c.log.Debug("trying to acquire lease key", zap.String("key", key), zap.Int64("lease_id", int64(leaseID)))

	txnResp, err := c.cli.Txn(ctx).
		If(clientv3.Compare(clientv3.CreateRevision(key), "=", 0)).
		Then(clientv3.OpPut(key, string(value), clientv3.WithLease(leaseID))).
		Commit()
	if err != nil {
		return false, err
	}

	// c.log.Debug("lease key acquire result", zap.String("key", key), zap.Bool("acquired", txnResp.Succeeded))
	return txnResp.Succeeded, nil
}
