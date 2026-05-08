# internal/runtime

Composition root. Wires all packages together and manages the agent lifecycle.

Startup sequence in `Run`:
1. Start API server (goroutine).
2. `connectETCD` — retry loop until etcd is reachable.
3. Load initial etcd snapshot into `store.StateStore`.
4. `bootstrap.Ensure` — seed missing etcd keys.
5. Start `watchLoop` (goroutine) — applies etcd watch events to the store.
6. Start `config.Watcher`, `lease.SessionManager`, `roles.Manager`, `lease.LeadershipManager` (goroutines).
7. `controllerWhenLeader` — starts `controller.Controller` each time leadership is acquired.

`putter.go` — `apiPutter` adapts `etcd.Client` to the `api.Putter` interface without creating a dependency from `api` to `etcd`.
