# internal/bootstrap

One-shot etcd seeding on agent startup.

`Bootstrapper.Ensure` writes initial cluster state to etcd using `TryPutIfAbsent` for shared keys (cluster config, role definitions, management group config, desired state) so only the first node to start wins. Node-own config (`config/nodes/{id}`) uses plain `Put` so name and membership changes propagate on restart.

Bootstrap does not create role actual/health keys — those are runtime keys owned by the session lease and must not exist in etcd between agent restarts.
