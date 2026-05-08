# internal/config

Local YAML config loading and etcd-based config overlay.

`config.go` — defines all config structs, loads and validates `config.yaml`. Notable:
- `RolesMap.UnmarshalYAML` extracts the reserved `defaults` key and applies it as a base to every role entry (zero-value fields inherit from defaults).
- `ActorCommand` accepts both YAML string (`"script.sh arg"`) and list form.
- `Merge` returns a shallow-copied `*Config` with etcd values overriding local where non-empty; node-local fields (endpoints, listen, node_id, actors_base_dir) are always taken from local.
- `ResolveActorPath` joins a relative actor path with `Node.ActorsBaseDir`.

`watcher.go` — `Watcher` watches the `config/{cluster_id}` prefix in etcd and rebuilds the merged config snapshot on each change. Currently logs the change; live propagation to running agents is a future concern.
