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

Обработчик проверяет поле `type` (должно быть `promote`, `disable` или `reload`) и обязательные поля `cluster_group` / `management_group`. Для `promote` дополнительно выполняется `validatePromote`:
- active-active топология (все группы имеют одинаковый приоритет) → `400`
- любая смежная группа имеет `desired=idle` **и** `actual=active|starting` → `409`
