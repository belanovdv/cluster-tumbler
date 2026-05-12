# internal/roles

Движок выполнения ролей на уровне узла. Работает независимо от лидерства.

`manager.go` — `Manager` запускает по одному `Worker` на каждую пару (management_group, роль) в членстве узла. Каждый воркер опрашивает состояние по захардкоженному внутреннему тикеру 500 мс и запускает выполнение при изменении `desired`, изменении флага `managed` или при истечении check interval (`roles.<id>.timeouts.check_interval`). При любом из условий вызывает `startDesiredExecution`, который отменяет текущее выполнение и запускает новую горутину.

`executor.go` — `RoleExecutor.Reconcile` диспетчеризует вызов к `ensure` для active/passive конвергенции. При `managed=false` вместо него вызывается `ReconcileDisabled`.

**Режим только наблюдения** (`managed=false`): `ReconcileDisabled` запускает probe, соответствующий `desired` (`probe_active` при desired=active, `probe_passive` при desired=passive), без каких-либо действий по конвергенции.
- Probe проходит → `actual=desired`, `health=ok`
- Probe упал → `actual=failed`, `health=failed`

В этом режиме акторы `set_*` и `force_stop` не вызываются. Группа наблюдается, но не управляется.

`converge.go` — `ensure` запускает цикл probe→set в рамках converge timeout (`roles.<id>.timeouts.converge`). Успех probe = завершено; exec error = немедленный сбой (без retry); ошибка exit-code = retry после retry interval (`roles.<id>.timeouts.retry_interval`). При истечении converge timeout вызывает `forceStop` для passive-переходов.

`actors.go` — `ExecActorRunner.Run` выполняет субпроцесс в рамках exec timeout (`roles.<id>.timeouts.exec`), внедряет переменные окружения `CT_*`, захватывает stdout/stderr, классифицирует ошибки как exec / exit-code / timeout.
