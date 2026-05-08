# internal/controller

Leader-only reconciliation loop. Runs only while this node holds the etcd leadership lease.

`Reconcile` iterates every management group and:
1. Checks for missing expected role state keys (agent-lost detection).
2. Aggregates individual role actual/health into a group-level actual/health.
3. Writes group actual and health back to etcd only when changed.

`applyPriorityPolicy` implements automatic failover: finds the available group with the lowest priority number and sets it to `active`; all others to `passive`.

Not implemented: auto failback, anti-flapping, quorum policy, command queue processing (reading `commands/{id}` written by the API, executing the requested desired-state change, and archiving to `commands_history/{id}`).
