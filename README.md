# cluster-tumbler

`cluster-tumbler` — агент оркестрации high availability on-premise кластеров.

Проект предназначен для управления состояниями конечных точек nodes через набор автоматов roles поддерживающих состояния
 `ACTIVE`, `PASSIVE`, `IDLE`, наблюдать фактическое состояние и координировать failover через общий согласованный backend.

На текущем этапе backend — ETCD.

## Простая идея

Каждый агент установлен на node.

Agent:
- читает локальную конфигурацию;
- регистрирует себя в ETCD;
- поддерживает session lease;
- публикует состояния своих ролей;
- может стать controller leader;
- отдает HTTP API и Web UI.

Controller leader:
- анализирует общее дерево ETCD;
- агрегирует состояние management groups;
- в automatic mode применяет priority-based failover.

## Основные сущности

### Cluster

Корень дерева:

```text
/{cluster_id}/cluster
```

### Cluster group
Логический контекст управления:

```text
/{cluster_id}/cluster/{cluster_group}
```
Примеры:
```text
geo_dc
rabbitmq
corevm
```

### Management group

Единица управления внутри cluster group.

Для DC-сценария:
```text
geo_dc/DC1
geo_dc/DC2
```
Для instance-сценария:
```text
rabbitmq/node1
rabbitmq/node2
rabbitmq/node3
```
### Desired
Предписание хранится на уровне management group:

```text
/{cluster_id}/cluster/{cluster_group}/{management_group}/desired
```
Пример:
```json
{
  "state": "active",
  "updated_at": "..."
}
```

## Role state
Фактическое состояние роли:

```text
/{cluster_id}/cluster/{cluster_group}/{management_group}/{node_id}/{role}/actual
/{cluster_id}/cluster/{cluster_group}/{management_group}/{node_id}/{role}/health
```

### Aggregate state

Controller рассчитывает состояние management group:
```text
/{cluster_id}/cluster/{cluster_group}/{management_group}/actual
/{cluster_id}/cluster/{cluster_group}/{management_group}/health
```
Правила текущей агрегации:
```
все роли active  -> management group active / ok
все роли passive -> management group passive / ok
есть failed      -> management group failed / failed
есть idle        -> management group idle / warning
mixed active/passive -> management group failed / failed
```
## Registry и session

Глобальная регистрация физического агента:
```
/{cluster_id}/cluster/registry/{node_id}
```

Глобальная live session агента:
```
/{cluster_id}/cluster/session/{node_id}
```
- `registry` описывает memberships агента.
- `session` живет под ETCD lease и отражает liveness агента.

## Config и priority

Конфигурация management group:
```
/{cluster_id}/cluster/config/{cluster_group}/{management_group}
```

Пример:
```json
{
  "priority": 1,
  "updated_at": "..."
}
```
Priority используется controller в `automatic` режиме.

## Priority-based failover

Если включено:
```yaml
cluster:
  failover_mode: automatic
```
Controller:
1. собирает management groups внутри cluster group;
2. исключает failed groups;
3. выбирает минимальный priority;
4. всем доступным groups с этим priority выставляет `desired=active`;
5. остальным выставляет `desired=passive`.

Если несколько groups имеют одинаковый лучший priority — все они становятся active.

### Mock role runtime

На текущем этапе реальные scripts не вызываются.

Mock worker:

1. читает desired своей management group;
2. ждет 1 секунду;
3. пишет role actual/health.

Пример:
```
desired=active  -> actual=active, health=ok
desired=passive -> actual=passive, health=ok
desired=idle    -> actual=idle, health=warning
```
## Реальные actors

В конфигурации уже поддерживается будущая модель:
```yaml
actors:
  probe_active: ./actors/core/probe_active.sh
  set_active: ./actors/core/set_active.sh
  probe_passive: ./actors/core/probe_passive.sh
  set_passive: ./actors/core/set_passive.sh
  force_stop: ./actors/core/force_stop.sh
```

Но на текущем этапе actors не исполняются.

## API 
### State
```
GET /api/v1/state
```
Возвращает pretty JSON view.

### Commands
```
POST /api/v1/commands
```
Создает command key, но обработчик command queue пока не реализован.

## Web UI
```
GET /
```
Draft UI показывает:

- cluster groups;
- management groups;
- selected management group details;
- nodes/roles;
- agent registry;
- online/offline session status.

Управление через UI пока не реализовано.

## Запуск
```bash
go mod tidy
go run ./cmd --config config.example.yaml
```
Открыть UI:
```
http://localhost:5080/
```
Открыть JSON:
```
http://localhost:5080/api/v1/state
```

## Текущий статус реализации
### Реализовано:

- Go 1.23;
- единый бинарник;
- ETCD adapter;
- one-watch state sync;
- in-memory StateStore;
- bootstrap;
- registry/session;
- leadership;
- controller aggregation;
- priority-based failover;
- mock role workers;
- JSON API;
- draft Web UI.

### Частично реализовано:

- command API;
- actor config model.

### Не реализовано:

- real actor execution;
- command queue processor;
- management UI;
- config UI;
- SSE live updates;
- auto failback policy;
- anti-flapping logic;