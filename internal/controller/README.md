# internal/controller

Leader-only reconciliation loop and command consumer. Both run only while this node holds the etcd leadership lease.

### controller.go — reconciliation loop

`Reconcile` iterates every management group and:
1. Checks for missing expected role state keys (agent-lost detection).
2. Aggregates individual role actual/health into a group-level actual/health.
3. Writes group actual and health back to etcd only when changed.

`applyPriorityPolicy` runs in both `manual` and `automatic` modes:
- **Active-active topology** (all groups share the same priority): sets all non-idle groups to `desired=active`.
- **Active-passive topology**: implements two-phase switchover — first waits for the current active group to reach `actual=passive|failed`, then activates the target group. Wait timeout is `max(check_interval + converge + exec)` across the group's roles (falls back to 30 s).
- **Automatic mode only**: `handleAutoFailover` detects when the top-priority group has failed and swaps its priority with the next available group, triggering re-routing on the next cycle.
- Groups with `desired=idle` are never changed by the controller in manual mode.

### commands.go — command consumer

`CommandConsumer` watches the `commands/` etcd prefix and processes incoming commands:

| Command | Action |
|---|---|
| `promote` | Swaps priorities so the target group becomes highest-priority; controller applies switchover on next reconcile |
| `disable` | Writes `desired=idle` for the group |
| `reload` | Writes `desired=passive`; role workers re-attempt passive convergence |

Each command transitions through `pending → running → completed|failed` and is archived to `commands_history/{id}`.

Not implemented: anti-flapping.
