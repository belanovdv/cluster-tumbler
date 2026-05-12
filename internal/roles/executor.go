// executor.go defines RoleExecutor and dispatches desired state to convergence (Reconcile) or probe-only mode (ReconcileDisabled).
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

// Reconcile is the entry point for role convergence; dispatches to ensure for active or passive.
func (e *RoleExecutor) Reconcile(ctx context.Context, req RoleRequest, onTransition func(RoleStatus)) RoleStatus {
	switch req.Desired {
	case "active":
		return e.ensure(ctx, req, ProbeActive, SetActive, "active", "starting", onTransition)
	case "passive":
		return e.ensure(ctx, req, ProbePassive, SetPassive, "passive", "stopping", onTransition)
	default:
		return failed("unsupported desired state")
	}
}

// ReconcileDisabled runs the probe corresponding to the desired state without any convergence actions.
// Used when disable_control=true: the worker observes state but does not attempt to change it.
// Probe passes → actual=desired, health=ok. Probe fails → actual=failed, health=failed.
func (e *RoleExecutor) ReconcileDisabled(ctx context.Context, req RoleRequest) RoleStatus {
	var probeActor ActorName
	switch req.Desired {
	case "active":
		probeActor = ProbeActive
	case "passive":
		probeActor = ProbePassive
	default:
		return failed("unsupported desired state for disabled control mode")
	}

	cmd, ok := e.Actors[probeActor]
	if !ok {
		return RoleStatus{
			State:  "failed",
			Health: "failed",
			Details: map[string]any{"message": "no probe actor configured"},
		}
	}

	res := e.Runner.Run(ctx, e.build(req, probeActor, cmd), 1)
	if res.ErrorType == ErrorExec {
		return failedActor(res)
	}
	if res.OK {
		return RoleStatus{
			State:  req.Desired,
			Health: "ok",
		}
	}

	return RoleStatus{
		State:  "failed",
		Health: "failed",
		Details: map[string]any{
			"message": "probe failed: actual state does not match desired",
			"stdout":  res.Stdout,
			"stderr":  res.Stderr,
		},
	}
}
