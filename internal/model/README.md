# internal/model

Domain types and etcd key scheme. No business logic.

`types.go` — JSON-serializable document structs stored in etcd: desired/actual/health state, registration, session, leadership, config documents, and `Command`.

`Command` / `CommandStatus` — producer side of a planned command queue. The intent is that any node (including non-leaders) can write a `Command` document to `commands/{id}` via the API; the leader then reads the queue, executes the command (e.g. changes `desired` of a management group), and moves the document to `commands_history/{id}`. **The consumer (queue processor) is not yet implemented.**

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
