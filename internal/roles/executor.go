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
