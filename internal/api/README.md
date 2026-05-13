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

The handler validates the `type` field (must be `promote`, `demote`, `disable`, `enable`, or `force_passive`) and the required `cluster_group` / `management_group` fields.

For `enable`, `validateEnable` checks:
- target group already has `managed=true` → `409`

For `promote`, `validatePromote` additionally checks:
- any sibling group has `managed=false` and `actual=active` or `actual=starting` → `409` (unmanaged active group cannot be drained by the controller; use `force_passive` first)

For `demote`, `validateDemote` checks:
- target group not found → `400`
- target has `desired=passive` → passes immediately (convergence reset, no further checks)
- any group in the cluster group has `actual=starting` or `actual=stopping` → `409` (cluster is transitioning)
- no passive managed group with `health=ok` available as replacement → `409`

For `force_passive`, `validateForcePassive` checks:
- target group has `managed=true` → `409`
- target group does not have `desired=active` → `409`
