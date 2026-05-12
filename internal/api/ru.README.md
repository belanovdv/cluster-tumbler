# internal/api

HTTP API сервер и точка подключения Web UI.

Эндпоинты:
- `GET /api/v1/state` — полное состояние кластера в виде форматированного JSON
- `GET /api/v1/stream` — Server-Sent Events поток; отправляет новый JSON состояния при каждом изменении хранилища
- `POST /api/v1/commands` — постановка команды управления в очередь; возвращает `202 Accepted` с ID команды
- `GET /` — встроенный HTML дашборд (обслуживается `internal/web`)
- `GET /assets/*` — встроенные статические ресурсы

`view.go` содержит `BuildStateView`, который преобразует дерево `store.TreeNode` из памяти в типизированную JSON-иерархию `StateView` (кластер → группа → группа управления → узел → роль).

Bearer-токен аутентификация опциональна: если `api.token` не задан, все запросы проходят без проверки.

### Валидация команд (`POST /api/v1/commands`)

Обработчик проверяет поле `type` (должно быть `promote`, `demote`, `disable`, `enable` или `force_passive`) и обязательные поля `cluster_group` / `management_group`.

Для `enable` выполняется `validateEnable`:
- целевая группа уже имеет `managed=true` → `409`

Для `promote` дополнительно выполняется `validatePromote`:
- active-active топология (все группы имеют одинаковый приоритет) → `400`
- любая смежная группа имеет `managed=false` и `actual=active` или `actual=starting` → `409` (неуправляемую активную группу контроллер не может остановить; сначала выполните `force_passive`)

Для `demote` выполняется `validateDemote`:
- целевая группа не найдена → `400`
- целевая группа имеет `desired=passive` → проходит сразу (повторная конвергенция, дальнейшие проверки не нужны)
- любая группа в кластерной группе имеет `actual=starting` или `actual=stopping` → `409` (кластер в процессе перехода)
- нет доступного пассивного управляемого кандидата с `health=ok` → `409`

Для `force_passive` выполняется `validateForcePassive`:
- целевая группа имеет `managed=true` → `409`
- целевая группа не имеет `desired=active` → `409`
