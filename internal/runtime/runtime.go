// Package runtime собирает все модули агента в единый lifecycle.
package runtime

import (
	"context"
	"time"

	"cluster-tumbler/internal/api"
	"cluster-tumbler/internal/bootstrap"
	"cluster-tumbler/internal/config"
	"cluster-tumbler/internal/controller"
	"cluster-tumbler/internal/etcd"
	"cluster-tumbler/internal/keys"
	"cluster-tumbler/internal/leadership"
	"cluster-tumbler/internal/logging"
	"cluster-tumbler/internal/roles"
	"cluster-tumbler/internal/session"
	"cluster-tumbler/internal/store"

	"go.uber.org/zap"
)

type Runtime struct {
	cfg *config.Config
	log *zap.Logger

	store      *store.StateStore
	etcdClient *etcd.Client

	api        *api.Server
	bootstrap  *bootstrap.Bootstrapper
	session    *session.Manager
	leader     *leadership.Manager
	controller *controller.Controller
	roles      *roles.Manager
}

// New создает и связывает все модули.
func New(cfg *config.Config) (*Runtime, error) {
	baseLogger, err := logging.New(cfg.Local.Logger)
	if err != nil {
		return nil, err
	}

	baseLogger = baseLogger.With(
		zap.String("cluster", cfg.Cluster.ID),
		zap.String("node", cfg.Agent.NodeID),
	)

	log := logging.WithComponent(baseLogger, "runtime")

	etcdClient, err := etcd.New(
		cfg.Local.Etcd.Endpoints,
		cfg.Local.Etcd.DialTimeout.Duration,
		logging.WithComponent(baseLogger, "etcd"),
	)
	if err != nil {
		return nil, err
	}

	st := store.New(logging.WithComponent(baseLogger, "store"))

	return &Runtime{
		cfg:        cfg,
		log:        log,
		store:      st,
		etcdClient: etcdClient,
		api: api.New(
			cfg.Local.API.Listen,
			cfg.Cluster.ID,
			st,
			apiPutter{client: etcdClient},
			logging.WithComponent(baseLogger, "api"),
		),
		bootstrap: bootstrap.New(
			cfg,
			etcdClient,
			logging.WithComponent(baseLogger, "bootstrap"),
		),
		session: session.New(
			cfg,
			etcdClient,
			logging.WithComponent(baseLogger, "session"),
		),
		leader: leadership.New(
			cfg,
			etcdClient,
			logging.WithComponent(baseLogger, "leadership"),
		),
		controller: controller.New(
			cfg,
			st,
			etcdClient,
			logging.WithComponent(baseLogger, "controller"),
		),
		roles: roles.New(
			cfg,
			st,
			etcdClient,
			logging.WithComponent(baseLogger, "roles"),
		),
	}, nil
}

// Run запускает API, ETCD sync, bootstrap, session, roles, leadership и controller.
func (r *Runtime) Run(ctx context.Context) error {
	r.log.Info("starting cluster-tumbler")

	go func() {
		if err := r.api.Run(ctx); err != nil {
			r.log.Error("api server failed", zap.Error(err))
		}
	}()

	if err := r.connectETCD(ctx); err != nil {
		return err
	}
	defer r.etcdClient.Close()

	root := keys.Root(r.cfg.Cluster.ID)

	items, revision, err := r.etcdClient.GetPrefix(ctx, root)
	if err != nil {
		return err
	}

	if err := r.store.LoadSnapshot(items, revision); err != nil {
		return err
	}

	if err := r.bootstrap.Ensure(ctx); err != nil {
		return err
	}

	go r.watchLoop(ctx, root, revision+1)

	go func() {
		if err := r.session.Run(ctx); err != nil && err != context.Canceled {
			r.log.Error("session manager failed", zap.Error(err))
		}
	}()

	go func() {
		if err := r.roles.Run(ctx); err != nil && err != context.Canceled {
			r.log.Error("role manager failed", zap.Error(err))
		}
	}()

	go func() {
		if err := r.leader.Run(ctx); err != nil && err != context.Canceled {
			r.log.Error("leadership manager failed", zap.Error(err))
		}
	}()

	go r.controllerWhenLeader(ctx)

	<-ctx.Done()

	r.log.Info("cluster-tumbler stopped")
	return ctx.Err()
}

func (r *Runtime) connectETCD(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		r.log.Debug("checking etcd connectivity")

		checkCtx, cancel := context.WithTimeout(ctx, r.cfg.Local.Etcd.DialTimeout.Duration)
		_, _, err := r.etcdClient.GetPrefix(checkCtx, keys.Root(r.cfg.Cluster.ID))
		cancel()

		if err == nil {
			r.log.Info("etcd connectivity established")
			return nil
		}

		r.log.Warn("etcd unavailable, retrying", zap.Error(err))

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(r.cfg.Local.Etcd.RetryInterval.Duration):
		}
	}
}

func (r *Runtime) watchLoop(ctx context.Context, prefix string, fromRevision int64) {
	events := r.etcdClient.WatchPrefix(ctx, prefix, fromRevision)

	for {
		select {
		case event, ok := <-events:
			if !ok {
				r.log.Warn("watch channel closed")
				return
			}

			if err := r.store.Apply(event); err != nil {
				r.log.Error("failed to apply watch event", zap.Error(err))
			}

		case <-ctx.Done():
			r.log.Debug("watch loop stopped")
			return
		}
	}
}

func (r *Runtime) controllerWhenLeader(ctx context.Context) {
	for {
		select {
		case event := <-r.leader.Events():
			r.log.Debug("leadership event received", zap.String("kind", event.Kind))

			if event.Kind == "acquired" {
				go func() {
					if err := r.controller.Run(ctx); err != nil && err != context.Canceled {
						r.log.Error("controller failed", zap.Error(err))
					}
				}()
			}

		case <-ctx.Done():
			return
		}
	}
}
