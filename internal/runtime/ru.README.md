# internal/runtime

Точка сборки (composition root). Связывает все пакеты и управляет жизненным циклом агента.

Последовательность запуска в `Run`:
1. Запуск API сервера (горутина).
2. `connectETCD` — цикл с retry до установления соединения с etcd.
3. Загрузка начального снапшота etcd в `store.StateStore`.
4. `bootstrap.Ensure` — заполнение отсутствующих ключей etcd.
5. Запуск `watchLoop` (горутина) — применяет события watch из etcd к хранилищу.
6. Запуск `config.Watcher`, `lease.SessionManager`, `roles.Manager`, `lease.LeadershipManager` (горутины).
7. `controllerWhenLeader` — запускает `controller.Controller` и `controller.CommandConsumer` каждый раз при получении лидерства.

`putter.go` — `apiPutter` адаптирует `etcd.Client` к интерфейсу `api.Putter`, не создавая зависимости от `etcd` в пакете `api`.
