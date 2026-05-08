# internal/store

In-memory read-through cache of etcd state, modelled as a path-indexed tree.

`StateStore` holds all etcd keys under the cluster root as a `TreeNode` tree (each path segment is a node). It is the single source of truth for the controller, role workers, and API handler — no component reads from etcd directly after startup.

Key operations:
- `LoadSnapshot` — replaces the full tree from a bulk etcd read; marks the store as ready.
- `Apply` — applies a single put/delete event from the etcd watch stream.
- `Snapshot` — returns a deep-copied tree safe for concurrent use by the API handler.
- `Get` / `Prefix` / `ListChildren` — point and range lookups without etcd round-trips.
- `Notify` — returns a buffered channel that receives a signal after every `Apply` or `LoadSnapshot`; used by the SSE stream handler.
