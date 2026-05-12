# internal/model

Доменные типы и схема ключей etcd. Без бизнес-логики.

`types.go` — JSON-сериализуемые документы, хранимые в etcd: состояния desired/actual/health, регистрация, сессия, лидерство, конфигурационные документы и `Command`.

`Command` / `CommandStatus` — документы очереди команд. Любой узел может записать `Command` в `commands/{id}` через API; `CommandConsumer` на лидере читает очередь, выполняет команду и архивирует её в `commands_history/{id}`.

`DesiredDocument` содержит два поля, вместе определяющих намерение управления:
- `state` — целевое состояние: `active` или `passive`
- `managed` — когда `true`, группа находится под нормальным управлением контроллера; когда `false` (умолчание при bootstrap), контроллер пропускает группу, а воркеры ролей работают в режиме только наблюдения (без конвергенции)

`keys.go` — чистые функции для построения путей ключей etcd. Иерархия ключей:
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
