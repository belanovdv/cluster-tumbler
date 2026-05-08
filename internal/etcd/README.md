# internal/etcd

Thin wrapper around the etcd v3 client.

Provides:
- `GetPrefix` / `WatchPrefix` — bulk read and streaming watch, returning `store.Event` values.
- `Put` / `PutWithLease` / `Delete` — basic KV writes.
- `TryPutIfAbsent` — conditional put via etcd transaction (create-revision == 0).
- `TryAcquireLeaseKey` — atomic leader election step: write key with lease only if key does not exist.
- `GrantLease` / `KeepAlive` — lease lifecycle.
- `SetSessionLeaseID` / `SessionLeaseID` / `ClearSessionLeaseID` — shared mutable state for the session lease ID, used by role workers to attach their writes to the active session.
