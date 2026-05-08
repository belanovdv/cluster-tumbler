// putter.go adapts etcd.Client to the api.Putter interface at the composition root.
package runtime

import (
	"context"

	"cluster-tumbler/internal/etcd"
)

type apiPutter struct {
	client *etcd.Client
}

func (p apiPutter) Put(ctx context.Context, key string, value []byte) error {
	return p.client.Put(ctx, key, value)
}
