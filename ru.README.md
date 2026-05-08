# cluster-tumbler

**Распределённая децентрализованная платформа управления on-premise инфраструктурой, построенная как сеть равноправных агентов с реактивной событийной моделью и распределённой конфигурацией в etcd.**

На каждом узле запускается один и тот же бинарный файл агента. Агенты синхронизируют состояние через etcd с помощью механизма watch и реактивно применяют конфигурационные предписания (desired state). Каждый узел предоставляет встроенный веб-интерфейс и HTTP API, что делает систему отказоустойчивой и не зависящей от центрального сервера управления.

---

## Архитектура

```
  Узел 1 (агент)          Узел 2 (агент)          Узел N (агент)
  ┌──────────────┐        ┌──────────────┐        ┌──────────────┐
  │  Web UI      │        │  Web UI      │        │  Web UI      │
  │  HTTP API    │        │  HTTP API    │        │  HTTP API    │
  │  Role workers│        │  Role workers│        │  Role workers│
  │  Session mgr │        │  Session mgr │        │  Session mgr │
  │  [Controller]│        │  [Controller]│        │  [Controller]│
  └──────┬───────┘        └──────┬───────┘        └──────┬───────┘
         │                       │                       │
         └───────────────────────┼───────────────────────┘
                                 │
                          ┌──────▼──────┐
                          │    etcd     │
                          └─────────────┘
```

Один агент в кластере удерживает **lease лидерства** и запускает **controller** — цикл согласования, агрегирующий состояние и применяющий политику failover. Все остальные агенты выполняют только воркеры ролей. Смена лидера происходит автоматически при истечении lease.

---

## Основные концепции

### Cluster
Корень дерева ключей в etcd. Идентифицируется параметром `cluster.id` в конфигурации.

### Cluster Group
Логический контекст управления — например, географическая площадка, тип сервиса или уровень инфраструктуры.
```
/{cluster_id}/cluster/{cluster_group}
```
Примеры: `geo_dc`, `rabbitmq`, `corevm`

### Management Group
Единица failover внутри cluster group. Каждая management group имеет собственный `desired` и агрегированные `actual`/`health`.
```
/{cluster_id}/cluster/{cluster_group}/{management_group}
```
Примеры (DC-сценарий): `geo_dc/DC1`, `geo_dc/DC2`
Примеры (instance-сценарий): `rabbitmq/node1`, `rabbitmq/node2`

### Desired / Actual / Health

**Desired** — предписанное состояние management group, устанавливается контроллером или через API:

| Значение | Смысл |
|---|---|
| `active` | Группа должна быть активна |
| `passive` | Группа должна быть пассивна (резерв) |
| `idle` | Группа в обслуживании / не сконфигурирована |

**Actual** — наблюдаемое агрегированное состояние всех ролей в группе:
`idle` · `starting` · `active` · `passive` · `stopping` · `failed`

**Health**: `ok` · `warning` · `failed`

### Role

Каждая management group определяет список ролей (например, `core`, `mc`, `postgresql`). Каждая роль выполняется воркером на каждом узле-члене и переходит между состояниями с помощью **скриптов-акторов**.

### Акторы

Каждая роль управляется до пяти скриптами-акторами:

| Актор | Назначение |
|---|---|
| `probe_active` | Проверить, активна ли роль; exit 0 = уже активна |
| `set_active` | Перевести роль в активное состояние |
| `probe_passive` | Проверить, пассивна ли роль; exit 0 = уже пассивна |
| `set_passive` | Перевести роль в пассивное состояние |
| `force_stop` | Аварийная остановка при истечении converge timeout |

Скрипты получают контекст через переменные окружения: `CT_ACTOR`, `CT_CLUSTER_GROUP`, `CT_MANAGEMENT_GROUP`, `CT_NODE_ID`, `CT_ROLE`, `CT_DESIRED`.

Цикл конвергенции: **probe → (если не готово) → set → retry** до успеха или истечения `timeouts.converge`.

---

## Конфигурация

Один YAML-файл на узел. Ключевые секции:

```yaml
etcd:
  endpoints: ["10.20.248.77:2379"]
  dial_timeout: 3s
  retry_interval: 1s

api:
  listen: ":5080"
  # token: "secret"       # опционально; если задан — все /api/v1/* требуют Authorization: Bearer <token>

cluster:
  id: my_cluster
  name: "My Cluster"
  groups:
    geo_dc:
      name: "Geographic DC"
  failover_mode: manual   # или: automatic
  leader_ttl: 2s
  leader_renew_interval: 500ms
  session_ttl: 30s

node:
  node_id: node1
  name: "Node 1"
  actors_base_dir: "."    # базовая директория для относительных путей к акторам
  disable_api: false        # true — не запускать HTTP API и Web UI на этом узле
  disable_controller: false # true — исключить узел из выборов лидера

  memberships:
    - cluster_group: geo_dc
      management_group: DC1

management_groups:
  geo_dc:
    DC1:
      priority: 1         # меньше = приоритетнее при automatic failover
      roles:
        - core
        - mc

roles:
  defaults:               # применяется ко всем ролям, если не переопределено
    timeouts:
      exec: 5s
      converge: 10s
      retry_interval: 1s
      check_interval: 5s
      details_max_size: 4096

  core:
    name: "Core"
    actors:
      probe_active:  "scripts/probe_active.sh core"
      set_active:    "scripts/set_active.sh core"
      probe_passive: "scripts/probe_passive.sh core"
      set_passive:   "scripts/set_passive.sh core"
      force_stop:    "scripts/force_stop.sh core"
```

Команды акторов принимают как строковую форму YAML (`"script.sh arg"`), так и список (`["script.sh", "arg"]`).

### Ограничение функциональности узла (небезопасные зоны)

`node.disable_api` и `node.disable_controller` предназначены для агентов, развёрнутых в недоверенных сетевых зонах. Соответствующие CLI-флаги переопределяют значения из файла конфигурации при запуске:

```bash
--disable-api         # не запускать HTTP API и Web UI
--disable-controller  # не участвовать в выборах лидера
```

> **Предупреждение:** Каждый узел с `disable_controller: true` уменьшает число кандидатов на роль контроллера. Если все узлы кластера имеют этот флаг, автоматический failover полностью перестаёт работать. Используйте эти опции только для узлов в недоверенных зонах, где риск безопасности перевешивает пользу от дублирования.

---

## Иерархия ключей etcd

```
/{cluster_id}/cluster/
  leadership
  registry/{node_id}              # регистрация узла (постоянная)
  session/{node_id}               # liveness агента (привязан к session lease)
  config/_meta                    # документ конфигурации кластера
  config/nodes/{node_id}
  config/roles/{role_id}
  config/cluster_groups/{cg}/_meta
  config/cluster_groups/{cg}/{mg} # конфиг management group (priority, roles)
  commands/{command_id}           # очередь команд — продюсер реализован
  commands_history/{command_id}   # архив команд — потребитель не реализован
  {cluster_group}/{mg}/desired
  {cluster_group}/{mg}/actual
  {cluster_group}/{mg}/health
  {cluster_group}/{mg}/{node_id}/{role}/actual
  {cluster_group}/{mg}/{node_id}/{role}/health
```

---

## Priority-Based Failover

При `failover_mode: automatic` controller на каждом цикле согласования:
1. Собирает все management groups внутри cluster group.
2. Исключает группы с `health=failed` или `actual=failed`.
3. Находит минимальное значение `priority` среди доступных групп.
4. Выставляет `desired=active` всем группам с этим приоритетом; остальным — `desired=passive`.

Если несколько групп имеют одинаковый лучший приоритет — все они становятся `active` одновременно.

### Правила агрегации состояния контроллером

| Наблюдаемые состояния ролей | Group actual | Group health |
|---|---|---|
| Все active, нет passive | `active` | `ok` |
| Все passive, нет active | `passive` | `ok` |
| Есть и active, и passive | `failed` | `failed` |
| Есть starting | `starting` | `warning` |
| Есть stopping | `stopping` | `warning` |
| Есть idle (нет failed) | `idle` | `warning` |
| Есть failed | `failed` | `failed` |
| Ожидаемые ключи ролей отсутствуют | `failed` | `failed` |

---

## Запуск

```bash
go mod tidy

# Узел 1
go run ./cmd/main.go \
  --config ./test/testdata/configs/config.node1.yaml \
  --etcd 10.20.248.77:2379

# Узел 2 — в недоверенной зоне: без API, без контроллера
go run ./cmd/main.go \
  --config ./test/testdata/configs/config.node2.yaml \
  --etcd 10.20.248.77:2379 \
  --disable-api \
  --disable-controller
```

Флаг `--etcd` можно указывать несколько раз; он переопределяет `etcd.endpoints` из файла конфигурации:
```bash
go run ./cmd/main.go --config config.yaml \
  --etcd 10.20.248.77:2379 \
  --etcd 10.20.248.78:2379
```

---

## API

| Метод | Путь | Описание |
|---|---|---|
| `GET` | `/api/v1/state` | Полное состояние кластера в виде форматированного JSON |
| `GET` | `/api/v1/stream` | SSE-поток; отправляет обновлённое состояние при каждом изменении в etcd |
| `POST` | `/api/v1/commands` | Запись команды в etcd (только продюсер; потребитель не реализован) |

Все `/api/v1/*` эндпоинты принимают заголовок `Authorization: Bearer <token>`, если в конфигурации задан `api.token`.

### POST /api/v1/commands

```json
{
  "type": "set_desired",
  "cluster_group": "geo_dc",
  "management_group": "DC1",
  "desired": "active"
}
```

---

## Web UI

Доступен по адресу `http://<узел>:5080/`

Одностраничный дашборд подключается к `/api/v1/stream` через `EventSource` и обновляется в реальном времени без перезагрузки страницы. Отображает:
- Метаданные кластера и текущего лидера.
- Cluster groups и management groups с состоянием desired/actual/health.
- Состояние ролей на каждом узле с деталями вывода акторов.
- Статус регистрации и сессий всех подключённых агентов.

Управление через UI и редактирование конфигурации пока не реализованы.

---

## Статус реализации

### Реализовано
- Go-бинарник; один процесс на узел
- etcd v3 адаптер с watch-based синхронизацией состояния
- In-memory `StateStore` (дерево с индексацией по пути)
- Bootstrap — заполнение ключей etcd при первом запуске
- Lifecycle реестра и сессии
- Выборы лидера через TTL lease в etcd
- Агрегация состояния контроллером
- Priority-based автоматический failover
- Реальное выполнение акторов (цикл конвергенции probe→set с таймаутом и retry)
- `roles.defaults` — базовые таймауты/акторы, общие для всех ролей
- `actors_base_dir` — настраиваемая базовая директория для путей к акторам
- Флаг `--etcd` (повторяемый, переопределяет конфиг)
- JSON API (`/api/v1/state`)
- SSE live-update поток (`/api/v1/stream`)
- API продюсера команд (`/api/v1/commands`)
- Встроенный Web UI с live-обновлением состояния

### Частично реализовано
- Очередь команд — только запись; потребитель на стороне лидера не реализован
- Config watcher — обнаруживает изменения конфигурации в etcd, но не распространяет их на запущенные воркеры

### Не реализовано
- Потребитель очереди команд (лидер читает `commands/`, выполняет, архивирует в `commands_history/`)
- Управление desired state через UI
- Редактор конфигурации в UI
- Просмотр истории / diff в UI
- Политика auto failback
- Логика anti-flapping
- Quorum-aware политика failover
