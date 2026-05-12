# internal/controller

Leader-only reconciliation loop and command consumer. Both run only while this node holds the etcd leadership lease.

### controller.go — reconciliation loop

`Reconcile` iterates every management group and:
1. Skips groups with `disable_control=true` — they are under manual administration.
2. Checks for missing expected role state keys (agent-lost detection).
3. Aggregates individual role actual/health into a group-level actual/health.
4. Writes group actual and health back to etcd only when changed.

`applyPriorityPolicy` runs in both `manual` and `automatic` modes:
- **Active-active topology** (all groups share the same priority): sets all managed (`disable_control=false`) groups to `desired=active`.
- **Active-passive topology**: implements two-phase switchover — first waits for the current active group to reach `actual=passive|failed`, then activates the target group. Wait timeout is `max(check_interval + converge + exec)` across the group's roles (falls back to 30 s).
- **Automatic mode only**: `handleAutoFailover` detects when the top-priority group has failed and swaps its priority with the next available managed group, triggering re-routing on the next cycle.
- Groups with `disable_control=true` are never changed by the controller.

`writeDesiredIfChanged` always writes `disable_control=false` — the controller only manages groups under its authority.

### commands.go — command consumer

`CommandConsumer` watches the `commands/` etcd prefix and processes incoming commands:

| Command | Action |
|---|---|
| `promote` | Swaps priorities so the target group becomes highest-priority; controller applies switchover on next reconcile |
| `disable` | Sets `disable_control=true` on the group, preserving the current `desired` state; controller and worker stop managing it |
| `reload` | Writes `desired=passive, disable_control=false`; role workers re-attempt passive convergence |
| `force_passive` | Requires `disable_control=true` and `desired=active`; writes `desired=passive, disable_control=false` to re-enter managed pool via passive convergence |

Each command transitions through `pending → running → completed|failed` and is archived to `commands_history/{id}`.
