package roles

import (
	"context"
	"time"
)

func (e *RoleExecutor) ensure(
	ctx context.Context,
	req RoleRequest,
	probe ActorName,
	set ActorName,
	target string,
) RoleStatus {

	deadlineCtx, cancel := context.WithTimeout(ctx, e.Converge)
	defer cancel()

	attempt := 0

	for {
		select {
		case <-deadlineCtx.Done():
			return failedWith("converge timeout", map[string]any{
				"target":  target,
				"attempt": attempt,
			})
		default:
		}

		attempt++

		// -------- PROBE --------
		if cmd, ok := e.Actors[probe]; ok {
			res := e.Runner.Run(deadlineCtx, e.build(req, probe, cmd))

			if res.OK {
				return success(target, attempt, res)
			}

			// ❗ NO retry if exec error
			if res.ErrorType == ErrorExec {
				return failedActor(res, attempt)
			}
		}

		// -------- SET --------
		if cmd, ok := e.Actors[set]; ok {
			res := e.Runner.Run(deadlineCtx, e.build(req, set, cmd))

			// ❗ NO retry if exec error
			if res.ErrorType == ErrorExec {
				return failedActor(res, attempt)
			}
		}

		time.Sleep(e.RetryInterval)
	}
}

func (e *RoleExecutor) forceStop(ctx context.Context, req RoleRequest) RoleStatus {
	cmd, ok := e.Actors[ForceStop]
	if !ok {
		return success("idle", 0, ActorResult{})
	}

	res := e.Runner.Run(ctx, e.build(req, ForceStop, cmd))

	if res.ErrorType == ErrorExec {
		return failedActor(res, 1)
	}

	if res.OK {
		return success("idle", 1, res)
	}

	return failedActor(res, 1)
}

func (e *RoleExecutor) build(req RoleRequest, name ActorName, cmd []string) ActorRequest {
	return ActorRequest{
		Name:            name,
		Command:         cmd,
		ClusterGroup:    req.ClusterGroup,
		ManagementGroup: req.ManagementGroup,
		NodeID:          req.NodeID,
		Role:            req.Role,
		Desired:         req.Desired,
	}
}

func success(state string, attempt int, res ActorResult) RoleStatus {
	return RoleStatus{
		State:  state,
		Health: "ok",
		Details: map[string]any{
			"attempt":     attempt,
			"stdout":      res.Stdout,
			"stderr":      res.Stderr,
			"duration_ms": res.Duration.Milliseconds(),
		},
	}
}

func failed(msg string) RoleStatus {
	return RoleStatus{
		State:  "failed",
		Health: "failed",
		Details: map[string]any{
			"error": msg,
		},
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

func failedActor(res ActorResult, attempt int) RoleStatus {
	return RoleStatus{
		State:  "failed",
		Health: "failed",
		Details: map[string]any{
			"attempt":     attempt,
			"error":       res.Error,
			"error_type":  res.ErrorType,
			"exit_code":   res.ExitCode,
			"stdout":      res.Stdout,
			"stderr":      res.Stderr,
			"duration_ms": res.Duration.Milliseconds(),
		},
	}
}
