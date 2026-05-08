# internal/api

HTTP API server and Web UI mounting point.

Endpoints:
- `GET /api/v1/state` — full cluster state as pretty-printed JSON
- `GET /api/v1/stream` — Server-Sent Events stream; pushes a new state JSON on every store change
- `POST /api/v1/commands` — write a new `Command` document to etcd and return its ID (producer side only; the queue consumer is not yet implemented)
- `GET /` — embedded HTML dashboard (served by `internal/web`)
- `GET /assets/*` — embedded static assets

`view.go` contains `BuildStateView`, which converts the in-memory `store.TreeNode` tree into a typed `StateView` JSON hierarchy (cluster → group → management group → node → role).

Bearer token auth is optional: when `api.token` is empty all requests pass through.
