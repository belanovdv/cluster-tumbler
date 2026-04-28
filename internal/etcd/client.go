// Package etcd — thin wrapper над clientv3.
package etcd

import (
	"context"

	clientv3 "go.etcd.io/etcd/client/v3"
	"go.uber.org/zap"
)

type Client struct {
	cli *clientv3.Client
	log *zap.Logger
}

func New(endpoints []string, log *zap.Logger) (*Client, error) {
	cli, err := clientv3.New(clientv3.Config{
		Endpoints: endpoints,
	})
	if err != nil {
		return nil, err
	}

	return &Client{cli: cli, log: log}, nil
}

func (c *Client) Put(ctx context.Context, key, val string, opts ...clientv3.OpOption) error {
	_, err := c.cli.Put(ctx, key, val, opts...)
	return err
}

func (c *Client) GetPrefix(ctx context.Context, prefix string) (*clientv3.GetResponse, error) {
	return c.cli.Get(ctx, prefix, clientv3.WithPrefix())
}

func (c *Client) WatchPrefix(ctx context.Context, prefix string) clientv3.WatchChan {
	return c.cli.Watch(ctx, prefix, clientv3.WithPrefix())
}

func (c *Client) Grant(ctx context.Context, ttl int64) (*clientv3.LeaseGrantResponse, error) {
	return c.cli.Grant(ctx, ttl)
}

func (c *Client) KeepAlive(ctx context.Context, leaseID clientv3.LeaseID) (<-chan *clientv3.LeaseKeepAliveResponse, error) {
	return c.cli.KeepAlive(ctx, leaseID)
}

func (c *Client) Txn(ctx context.Context) clientv3.Txn {
	return c.cli.Txn(ctx)
}
