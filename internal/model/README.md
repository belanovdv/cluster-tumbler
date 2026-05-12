# internal/model

Domain types and etcd key scheme. No business logic.

`types.go` — JSON-serializable document structs stored in etcd: desired/actual/health state, registration, session, leadership, config documents, and `Command`.

`Command` / `CommandStatus` — command queue documents. Any node can write a `Command` to `commands/{id}` via the API; the leader's `CommandConsumer` reads the queue, executes the command, and archives it to `commands_history/{id}`.

`DesiredDocument` carries two fields that together define management intent:
- `state` — target state: `active` or `passive`
- `disable_control` — when `true`, the controller skips this group entirely and the role worker runs in probe-only mode (no convergence)

`keys.go` — pure functions that construct etcd key paths. The key hierarchy is:
```
/{cluster_id}/cluster/
  leadership
  registry/{node_id}
  session/{node_id}
  config/_meta
  config/nodes/{node_id}
  config/roles/{role_id}
  config/cluster_groups/{cg_id}/_meta
  config/cluster_groups/{cg_id}/{mg_id}
  commands/{command_id}
  commands_history/{command_id}
  {cluster_group}/{management_group}/desired
  {cluster_group}/{management_group}/actual
  {cluster_group}/{management_group}/health
  {cluster_group}/{management_group}/{node_id}/{role}/actual
  {cluster_group}/{management_group}/{node_id}/{role}/health
```
