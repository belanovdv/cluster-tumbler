# internal/roles

Движок выполнения ролей на уровне узла. Работает независимо от лидерства.

`manager.go` — `Manager` запускает по одному `Worker` на каждую пару (management_group, роль) в членстве узла. Каждый воркер опрашивает состояние по захардкоженному внутреннему тикеру 500 мс и запускает выполнение при изменении desired или при истечении check interval (`roles.<id>.timeouts.check_interval`). При любом из условий вызывает `startDesiredExecution`, который отменяет текущее выполнение и запускает новую горутину.

`executor.go` — `RoleExecutor.Reconcile` диспетчеризует вызов к `ensure` для active/passive или к `reconcileIdle` для idle.

**Idle режим** (`desired=idle`): `reconcileIdle` запускает `probe_active` при каждом check interval.
- Probe проходит → `actual=active`, `health=warning` — сервисы работают, но не управляются; удерживает пока probe не упадёт.
- Probe упал → запускает `set_passive` в рамках `converge` timeout (fallback: `force_stop`) → `actual=idle`.

Благодаря этому группа остаётся видимо `actual=active` пока сервисы живы, и валидация promote корректно блокирует переключение. Как только сервисы останавливаются (probe падает), воркер выполняет разовую очистку и снимает управление.

`converge.go` — `ensure` запускает цикл probe→set в рамках converge timeout (`roles.<id>.timeouts.converge`). Успех probe = завершено; exec error = немедленный сбой (без retry); ошибка exit-code = retry после retry interval (`roles.<id>.timeouts.retry_interval`). При истечении converge timeout вызывает `forceStop` для passive-переходов.

`actors.go` — `ExecActorRunner.Run` выполняет субпроцесс в рамках exec timeout (`roles.<id>.timeouts.exec`), внедряет переменные окружения `CT_*`, захватывает stdout/stderr, классифицирует ошибки как exec / exit-code / timeout.
