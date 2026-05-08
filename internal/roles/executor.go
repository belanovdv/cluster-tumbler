// executor.go defines RoleExecutor and dispatches desired state to convergence helpers.
package roles

import (
	"context"
	"time"
)

type RoleExecutor struct {
	Runner        *ExecActorRunner
	Actors        map[ActorName][]string
	Converge      time.Duration
	RetryInterval time.Duration
}

type RoleRequest struct {
	ClusterGroup    string
	ManagementGroup string
	NodeID          string
	Role            string
	Desired         string
}

type RoleStatus struct {
	State   string
	Health  string
	Details map[string]any
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

// forceStop runs the force_stop actor to gracefully terminate the role during passive convergence timeout.
func (e *RoleExecutor) forceStop(ctx context.Context, req RoleRequest) RoleStatus {
	cmd, ok := e.Actors[ForceStop]
	if !ok {
		return success("idle", ActorResult{})
	}

	res := e.Runner.Run(ctx, e.build(req, ForceStop, cmd), 1)

	if res.ErrorType == ErrorExec {
		return failedActor(res)
	}

	if res.OK {
		return success("idle", res)
	}

	return failedActor(res)
}

// Reconcile is the entry point for role convergence; dispatches to ensure (active/passive) or reconcileIdle.
func (e *RoleExecutor) Reconcile(ctx context.Context, req RoleRequest, onTransition func(RoleStatus)) RoleStatus {
	switch req.Desired {
	case "active":
		return e.ensure(ctx, req, ProbeActive, SetActive, "active", "starting", onTransition)
	case "passive":
		return e.ensure(ctx, req, ProbePassive, SetPassive, "passive", "stopping", onTransition)
	case "idle":
		return e.reconcileIdle(ctx, req)
	default:
		return failed("unsupported desired state")
	}
}

// reconcileIdle implements the idle probe loop: holds actual=active while probe_active passes;
// on probe failure runs set_passive (force_stop fallback) and reports actual=idle.
func (e *RoleExecutor) reconcileIdle(ctx context.Context, req RoleRequest) RoleStatus {
	if cmd, ok := e.Actors[ProbeActive]; ok {
		res := e.Runner.Run(ctx, e.build(req, ProbeActive, cmd), 1)
		if res.ErrorType == ErrorExec {
			return failedActor(res)
		}
		if res.OK {
			return RoleStatus{
				State:  "active",
				Health: "warning",
				Details: map[string]any{
					"message": "Unmanaged: services active (desired=idle)",
					"stdout":  res.Stdout,
					"stderr":  res.Stderr,
				},
			}
		}
	}

	// probe_active failed or not configured — ensure services are stopped, then hand off.
	deadlineCtx, cancel := context.WithTimeout(ctx, e.Converge)
	defer cancel()

	stopped := false
	if cmd, ok := e.Actors[SetPassive]; ok {
		res := e.Runner.Run(deadlineCtx, e.build(req, SetPassive, cmd), 1)
		if res.ErrorType != ErrorExec && res.OK {
			stopped = true
		}
	}
	if !stopped {
		e.forceStop(ctx, req)
	}

	return RoleStatus{
		State:   "idle",
		Health:  "warning",
		Details: map[string]any{"message": "Idle"},
	}
}
