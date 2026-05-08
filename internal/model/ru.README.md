# internal/model

Доменные типы и схема ключей etcd. Без бизнес-логики.

`types.go` — JSON-сериализуемые документы, хранимые в etcd: состояния desired/actual/health, регистрация, сессия, лидерство, конфигурационные документы и `Command`.

`Command` / `CommandStatus` — продюсер-сторона запланированной очереди команд. Идея в том, что любой узел (включая не-лидеры) может записать документ `Command` в `commands/{id}` через API; лидер затем читает очередь, выполняет команду (например, меняет `desired` у группы управления) и перемещает документ в `commands_history/{id}`. **Потребитель (обработчик очереди) пока не реализован.**

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
