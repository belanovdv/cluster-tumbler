# internal/roles

Per-node role execution engine. Runs independently of leadership.

`manager.go` — `Manager` spawns one `Worker` per (management_group, role) membership. Each worker polls on a hardcoded 500ms internal ticker and triggers execution on desired change or when the check interval (`roles.<id>.timeouts.check_interval`) elapses. On either condition it calls `startDesiredExecution`, which cancels any in-flight run and starts a new goroutine.

`executor.go` — `RoleExecutor.Reconcile` dispatches to `ensure` for active/passive, or returns idle status.

`converge.go` — `ensure` runs a probe→set loop within the converge timeout (`roles.<id>.timeouts.converge`). Probe success = done; exec error = fail immediately (no retry); exit-code failure = retry after the retry interval (`roles.<id>.timeouts.retry_interval`). On converge timeout, calls `forceStop` for passive transitions.

`actors.go` — `ExecActorRunner.Run` executes a subprocess within the exec timeout (`roles.<id>.timeouts.exec`), injects `CT_*` environment variables, captures stdout/stderr, classifies errors into exec/exit-code/timeout.
