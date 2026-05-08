// converge.go implements the probe→set convergence loop used by RoleExecutor.
package roles

import (
	"context"
	"time"
)

// ensure is the main convergence loop: probe → (notify transition) → set, retrying until success or converge timeout.
func (e *RoleExecutor) ensure(
	ctx context.Context,
	req RoleRequest,
	probe ActorName,
	set ActorName,
	target string,
	transitionState string,
	onTransition func(RoleStatus),
) RoleStatus {

	deadlineCtx, cancel := context.WithTimeout(ctx, e.Converge)
	defer cancel()

	attempt := 0
	transitioned := false

	for {
		select {
		case <-deadlineCtx.Done():
			if target == "passive" {
				e.forceStop(ctx, req)
			}
			return failedWith("converge timeout", map[string]any{
				"target":  target,
				"attempt": attempt,
			})
		default:
		}

		attempt++

		// ---------- PROBE ----------
		if cmd, ok := e.Actors[probe]; ok {
			res := e.Runner.Run(deadlineCtx, e.build(req, probe, cmd), attempt)

			if res.OK {
				return success(target, res)
			}

			// ❗ no retry on exec_error
			if res.ErrorType == ErrorExec {
				return failedActor(res)
			}
		}

		// Notify transition on first detected mismatch
		if !transitioned {
			transitioned = true
			if onTransition != nil {
				onTransition(RoleStatus{
					State:  transitionState,
					Health: "warning",
					Details: map[string]any{
						"message":    "Convergence in progress",
						"target":     target,
						"started_at": time.Now().UTC().Format(time.RFC3339),
					},
				})
			}
		}

		// ---------- SET ----------
		if cmd, ok := e.Actors[set]; ok {
			res := e.Runner.Run(deadlineCtx, e.build(req, set, cmd), attempt)

			// ❗ no retry on exec_error
			if res.ErrorType == ErrorExec {
				return failedActor(res)
			}
		}

		time.Sleep(e.RetryInterval)
	}
}
func success(state string, res ActorResult) RoleStatus {
	return RoleStatus{
		State:  state,
		Health: "ok",
		Details: map[string]any{
			"attempt":     res.Attempt,
			"stdout":      res.Stdout,
			"stderr":      res.Stderr,
			"duration_ms": res.Duration.Milliseconds(),
		},
	}
}

func failedActor(res ActorResult) RoleStatus {
	return RoleStatus{
		State:  "failed",
		Health: "failed",
		Details: map[string]any{
			"attempt":     res.Attempt,
			"error":       res.Error,
			"error_type":  res.ErrorType,
			"exit_code":   res.ExitCode,
			"stdout":      res.Stdout,
			"stderr":      res.Stderr,
			"duration_ms": res.Duration.Milliseconds(),
		},
	}
}

func failed(msg string) RoleStatus {
	return RoleStatus{
		State:   "failed",
		Health:  "failed",
		Details: map[string]any{"error": msg},
	}
}

func failedWith(msg string, d map[string]any) RoleStatus {
	d["error"] = msg
	return RoleStatus{
		State:   "failed",
		Health:  "failed",
		Details: d,
	}
}
