# cluster-tumbler

**Decentralised high-availability cluster management platform built on a peer agent network with a reactive event model and distributed configuration in etcd.**

Each node runs an identical agent binary. Agents synchronise state through etcd using a watch-based event mechanism and reactively apply configuration prescriptions (desired states). Every node exposes its own built-in HTTP API and a draft monitoring Web UI, making the system resilient to single-node failures and independent of any central management server.

---

## Architecture

```
  Node 1 (agent)          Node 2 (agent)          Node N (agent)
  ┌──────────────┐        ┌──────────────┐        ┌──────────────┐
  │  Web UI      │        │  Web UI      │        │  Web UI      │
  │  HTTP API    │        │  HTTP API    │        │  HTTP API    │
  │  Role workers│        │  Role workers│        │  Role workers│
  │  Session mgr │        │  Session mgr │        │  Session mgr │
  │  [Controller]│        │  [Controller]│        │  [Controller]│
  └──────┬───────┘        └──────┬───────┘        └──────┬───────┘
         │                       │                       │
         └───────────────────────┼───────────────────────┘
                                 │
                          ┌──────▼──────┐
                          │    etcd     │
                          └─────────────┘
```

One agent per cluster holds the **leadership lease** and runs the **controller** — the reconciliation loop that aggregates state and applies failover policy. All other agents run role workers only. Leadership changes automatically on lease expiry.

---

## Core Concepts

### Cluster
The root of the etcd key tree. Identified by `cluster.id` in config.

### Cluster Group
A logical management context — e.g. a geographic location, a service type, or an infrastructure layer.
```
/{cluster_id}/cluster/{cluster_group}
```
Examples: `geo_dc`, `rabbitmq`, `corevm`

### Management Group
The unit of failover within a cluster group. Each management group has its own `desired` state and aggregated `actual`/`health`.
```
/{cluster_id}/cluster/{cluster_group}/{management_group}
```
Examples (DC scenario): `geo_dc/DC1`, `geo_dc/DC2`
Examples (instance scenario): `rabbitmq/node1`, `rabbitmq/node2`

### Desired / Actual / Health States

**Desired** — the prescribed state for a management group, set by the controller or via the API:

| Value | Meaning |
|---|---|
| `active` | Group should be active |
| `passive` | Group should be passive (standby) |

An additional boolean flag `managed` controls whether the group is under controller authority. When `false` (default at bootstrap), the controller skips the group and role workers run in probe-only mode — the group is observed but not controlled. When `true`, the controller manages the group normally. Use `disable` to set `managed=false`; use `enable` or `force_passive` to restore `managed=true`.

**Actual** — the observed aggregate state of all roles in the group:
`active` · `passive` · `starting` · `stopping` · `failed`

**Health**: `ok` · `warning` · `failed`

### Role

Each management group defines a list of roles (e.g. `core`, `mc`, `postgresql`). Each role is executed by a worker on every member node and transitions through states using **actor scripts**.

### Actors

Each role is driven by up to five actor scripts:

| Actor | Purpose |
|---|---|
| `probe_active` | Check if role is active; exit 0 = already active |
| `set_active` | Transition role to active |
| `probe_passive` | Check if role is passive; exit 0 = already passive |
| `set_passive` | Transition role to passive |
| `force_stop` | Emergency stop used on converge timeout |

Scripts receive context via environment variables: `CT_ACTOR`, `CT_CLUSTER_GROUP`, `CT_MANAGEMENT_GROUP`, `CT_NODE_ID`, `CT_ROLE`, `CT_DESIRED`.

Convergence loop: **probe → (if not done) → set → retry** until success or `timeouts.converge` expires.

---

## Configuration

A single YAML file per node. Key sections:

```yaml
etcd:
  endpoints: ["10.20.248.77:2379"]
  dial_timeout: 3s
  retry_interval: 1s

api:
  listen: ":5080"
  # token: "secret"       # optional Bearer token for /api/v1/*

cluster:
  id: my_cluster
  name: "My Cluster"
  groups:
    geo_dc:
      name: "Geographic DC"
  failover_mode: manual   # or: automatic
  leader_ttl: 2s
  leader_renew_interval: 500ms
  session_ttl: 30s

node:
  node_id: node1
  name: "Node 1"
  actors_base_dir: "."    # base directory for relative actor paths
  disable_api: false        # set true to suppress HTTP API and Web UI on this node
  managedler: false # set true to exclude this node from leader election

  memberships:
    - cluster_group: geo_dc
      management_group: DC1

management_groups:
  geo_dc:
    DC1:
      priority: 1         # lower = preferred in automatic failover
      roles:
        - core
        - mc

roles:
  defaults:               # applied to all roles unless overridden
    timeouts:
      exec: 5s
      converge: 10s
      retry_interval: 1s
      check_interval: 5s
      details_max_size: 4096

  core:
    name: "Core Service"
    actors:
      probe_active:  "scripts/probe_active.sh core"
      set_active:    "scripts/set_active.sh core"
      probe_passive: "scripts/probe_passive.sh core"
      set_passive:   "scripts/set_passive.sh core"
      force_stop:    "scripts/force_stop.sh core"
```

Actor commands accept both string (`"script.sh arg"`) and list (`["script.sh", "arg"]`) YAML forms.

### Restricting a Node (Unsafe Zones)

`node.disable_api` and `node.managedler` are intended for agents deployed in untrusted network zones. The corresponding CLI flags override the config file values at startup:

```bash
--disable-api         # do not start HTTP API and Web UI
--disable-controller  # do not participate in leader election
```

> **Warning:** Every node with `managedler: true` reduces the number of candidates for the controller role. If all nodes in a cluster have it set, automatic failover stops working entirely. Use these options only for nodes in untrusted zones where the security risk outweighs the redundancy benefit.

---

## etcd Key Hierarchy

```
/{cluster_id}/cluster/
  leadership
  registry/{node_id}              # node registration (persistent)
  session/{node_id}               # node liveness (session lease-bound)
  config/_meta                    # cluster config document
  config/nodes/{node_id}
  config/roles/{role_id}
  config/cluster_groups/{cg}/_meta
  config/cluster_groups/{cg}/{mg} # management group config (priority, roles)
  commands/{command_id}           # pending/running command queue
  commands_history/{command_id}   # archived completed/failed commands
  {cluster_group}/{mg}/desired
  {cluster_group}/{mg}/actual
  {cluster_group}/{mg}/health
  {cluster_group}/{mg}/{node_id}/{role}/actual
  {cluster_group}/{mg}/{node_id}/{role}/health
```

---

## Priority-Based Failover

When `failover_mode: automatic` the controller on each reconcile cycle:
1. Collects all management groups within a cluster group.
2. Excludes groups with `health=failed` or `actual=failed`.
3. Finds the lowest `priority` value among available groups.
4. Sets `desired=active` for all groups at that priority; `desired=passive` for the rest.

If multiple groups share the best priority they all become `active` simultaneously.

### Controller State Aggregation Rules

| Role states observed | Group actual | Group health |
|---|---|---|
| All active, none passive | `active` | `ok` |
| All passive, none active | `passive` | `ok` |
| Mix of active and passive | `failed` | `failed` |
| Any starting | `starting` | `warning` |
| Any stopping | `stopping` | `warning` |
| Any failed | `failed` | `failed` |
| Expected role keys missing | `failed` | `failed` |

---

## Running

```bash
go mod tidy

# Node 1
go run ./cmd/main.go \
  --config ./test/testdata/configs/config.node1.yaml \
  --etcd 10.20.248.77:2379

# Node 2 — in an untrusted zone: no API, no controller
go run ./cmd/main.go \
  --config ./test/testdata/configs/config.node2.yaml \
  --etcd 10.20.248.77:2379 \
  --disable-api \
  --disable-controller
```

`--etcd` is repeatable and overrides `etcd.endpoints` from the config file:
```bash
go run ./cmd/main.go --config config.yaml \
  --etcd 10.20.248.77:2379 \
  --etcd 10.20.248.78:2379
```

---

## API

| Method | Path | Description |
|---|---|---|
| `GET` | `/api/v1/state` | Full cluster state as pretty-printed JSON |
| `GET` | `/api/v1/stream` | Server-Sent Events stream; pushes updated state on every etcd change |
| `POST` | `/api/v1/commands` | Enqueue a management command; processed by the leader consumer |

All `/api/v1/*` endpoints accept an optional `Authorization: Bearer <token>` header when `api.token` is configured.

### POST /api/v1/commands

Five command types are supported:

| Type | Action |
|---|---|
| `promote` | Swap priorities so the target group becomes highest-priority; controller performs two-phase switchover |
| `demote` | Strip the specified active group of priority; auto-selects the best passive replacement (`managed=true`, `actual=passive`, `health=ok`, highest priority) and initiates switchover. If the group already has `desired=passive`, re-triggers passive convergence instead |
| `disable` | Set `managed=false`, preserving the current `desired` state — removes the group from controller authority and switches role workers to probe-only mode |
| `enable` | Set `managed=true`, preserving the current `desired` state — returns the group to normal management; inverse of `disable` |
| `force_passive` | Requires `managed=false` and `desired=active`; writes `desired=passive, managed=true` so role workers run `set_passive` and bring services down before the group can be re-enabled |

```bash
# Promote DC2 to highest priority (failover to specific target)
curl -X POST http://localhost:5080/api/v1/commands \
  -H "Content-Type: application/json" \
  -d '{"type":"promote","cluster_group":"geo_dc","management_group":"DC2"}'

# Demote DC1 (controller auto-selects best passive replacement)
curl -X POST http://localhost:5080/api/v1/commands \
  -H "Content-Type: application/json" \
  -d '{"type":"demote","cluster_group":"geo_dc","management_group":"DC1"}'

# Re-trigger passive convergence on a stuck passive group
curl -X POST http://localhost:5080/api/v1/commands \
  -H "Content-Type: application/json" \
  -d '{"type":"demote","cluster_group":"geo_dc","management_group":"DC1"}'

# Take DC1 out of rotation (maintenance mode)
curl -X POST http://localhost:5080/api/v1/commands \
  -H "Content-Type: application/json" \
  -d '{"type":"disable","cluster_group":"geo_dc","management_group":"DC1"}'

# Return DC1 to normal management (re-enable after maintenance)
curl -X POST http://localhost:5080/api/v1/commands \
  -H "Content-Type: application/json" \
  -d '{"type":"enable","cluster_group":"geo_dc","management_group":"DC1"}'

# Gracefully stop services on a disabled-but-active DC1 before maintenance
curl -X POST http://localhost:5080/api/v1/commands \
  -H "Content-Type: application/json" \
  -d '{"type":"force_passive","cluster_group":"geo_dc","management_group":"DC1"}'
```

**Response codes:** `202 Accepted` — command queued; `409 Conflict` — blocked by current state; `400 Bad Request` — invalid parameters.

**`promote` constraints:**
- Blocked if any sibling has `managed=false` and `actual=active` or `actual=starting` (unmanaged active group cannot be drained; use `force_passive` first).

**`demote` constraints:**
- Blocked if any group in the cluster group has `actual=starting` or `actual=stopping` (cluster is transitioning).
- Blocked if no passive managed group with `health=ok` is available as replacement.
- Passes without further checks when the target group already has `desired=passive`.

**`force_passive` constraints:**
- Blocked if `managed=true` — the group is under normal management; use `demote` instead.
- Blocked if `desired=passive` — services are already targeted to be passive.

**Switchover consistency:** the controller uses a two-phase approach — it waits for the stopping group to reach `actual=passive|failed` before activating the target. Wait timeout is derived from role timeouts: `max(check_interval + converge + exec)` across the group's roles.

---

## Web UI

> **Draft.** The current UI is a preliminary implementation for state visibility only. Full UI development — including design, authentication, and control elements — is planned but not yet started.

Available at `http://<node>:5080/`

The single-page dashboard connects to `/api/v1/stream` via `EventSource` and updates in real time without page reload. It shows:
- Cluster metadata and current leader.
- Cluster groups and management groups with desired/actual/health state.
- Per-node role states with actor output details.
- Registry and session status of all connected agents.

Not yet implemented in UI:
- Command controls (promote, demote, disable, enable, force_passive)
- Config editor
- History and diff view
- Authentication / access control

---

## Implementation Status

### Implemented
- Go binary; single process per node
- etcd v3 adapter with watch-based state sync
- In-memory `StateStore` (path-indexed tree)
- Bootstrap — seeds etcd keys on first start
- Registry / session lifecycle
- Leadership election via etcd TTL lease
- Controller state aggregation
- Priority-based failover with two-phase switchover (stops current active before starting new)
- Automatic priority swap on failure (`failover_mode: automatic`)
- Real actor execution (probe→set convergence loop with timeout and retry)
- `roles.defaults` — base timeouts/actors shared across roles
- `actors_base_dir` — configurable base directory for actor paths
- `--etcd` CLI flag (repeatable, overrides config)
- `--disable-api` / `--disable-controller` CLI flags (unsafe zone support)
- JSON API (`/api/v1/state`)
- SSE live-update stream (`/api/v1/stream`)
- Command API (`/api/v1/commands`) — `promote`, `demote`, `disable`, `enable`, `force_passive` with leader-side consumer

### Partially Implemented
- Draft Web UI — live state monitoring only
- Config watcher — detects etcd config changes but does not propagate to running workers

### Not Implemented
- Full Web UI (design, authentication, control elements — desired state, config editor, history/diff)
- Anti-flapping logic — prevents the controller from rapidly toggling `desired` when a group's health oscillates; requires a stabilisation window before a failover decision is applied
