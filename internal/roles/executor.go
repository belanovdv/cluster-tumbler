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

func (e *RoleExecutor) Reconcile(ctx context.Context, req RoleRequest) RoleStatus {
	switch req.Desired {
	case "active":
		return e.ensure(ctx, req, ProbeActive, SetActive, "active")
	case "passive":
		return e.ensure(ctx, req, ProbePassive, SetPassive, "passive")
	case "idle":
		return e.forceStop(ctx, req)
	default:
		return failed("unsupported desired state")
	}
}
