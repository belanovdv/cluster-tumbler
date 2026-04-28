// Package runtime putter adapter адаптирует ETCD Client под API Putter.
package runtime

import (
	"context"

	"cluster-tumbler/internal/etcd"
)

type apiPutter struct {
	client *etcd.Client
}

// Put записывает команду API в ETCD.
func (p apiPutter) Put(ctx context.Context, key string, value []byte) error {
	return p.client.Put(ctx, key, value)
}
