# internal/api

HTTP API server and Web UI mounting point.

Endpoints:
- `GET /api/v1/state` — full cluster state as pretty-printed JSON
- `GET /api/v1/stream` — Server-Sent Events stream; pushes a new state JSON on every store change
- `POST /api/v1/commands` — enqueue a management command; returns `202 Accepted` with command ID
- `GET /` — embedded HTML dashboard (served by `internal/web`)
- `GET /assets/*` — embedded static assets

`view.go` contains `BuildStateView`, which converts the in-memory `store.TreeNode` tree into a typed `StateView` JSON hierarchy (cluster → group → management group → node → role).

Bearer token auth is optional: when `api.token` is empty all requests pass through.

### Command validation (`POST /api/v1/commands`)

The handler validates the `type` field (must be `promote`, `disable`, or `reload`) and the required `cluster_group` / `management_group` fields. For `promote`, `validatePromote` additionally checks:
- active-active topology (all groups equal priority) → `400`
- any sibling group has `desired=idle` **and** `actual=active|starting` → `409`
