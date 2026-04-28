# cluster-agent

Current generated scope:

- Go 1.23
- ETCD adapter: snapshot, watch, put/delete, lease
- session lease heartbeat
- leader election
- tree StateStore
- API `/` and `/api/v1/state`
- API command creation `/api/v1/commands`
- basic GeoDC controller aggregation
- component-based zap logging without file:line caller

Run:

```bash
go mod tidy
go run ./cmd --config config.example.yaml
```

API:

```bash
curl http://localhost:5080/
curl http://localhost:5080/api/v1/state
```

Create command:

```bash
curl -X POST http://localhost:5080/api/v1/commands \
  -H 'Content-Type: application/json' \
  -d '{"cluster_group":"geo_dc","management_group":"DC1","desired":"active"}'
```


## Semantic aggregate writes

Controller writes management group `actual` / `health` only when semantic fields change:

- `actual.state`
- `actual.details`
- `health.status`
- `health.details`

`updated_at` is refreshed only on semantic change to avoid unnecessary ETCD watch churn.
