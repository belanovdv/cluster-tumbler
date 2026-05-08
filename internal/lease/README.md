# internal/lease

Manages the two etcd lease types used by the agent.

`session.go` — `SessionManager` grants a TTL lease (`cluster.session_ttl`), writes the node registration (persistent) and session key (lease-bound) to etcd, then runs KeepAlive for the lifetime of the context. The session lease ID is stored in `etcd.Client` so role workers can attach their actual/health writes to the same lease.

`leadership.go` — `LeadershipManager` tries to acquire the cluster leadership key via `TryAcquireLeaseKey` on each tick (`cluster.leader_renew_interval`). The leadership key is written with a short TTL lease (`cluster.leader_ttl`). On success it runs KeepAlive and emits an `"acquired"` event; on loss it emits `"lost"`. The runtime subscribes to these events to start/stop the controller goroutine.
