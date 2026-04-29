package roles

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"time"
)

type ExecActorRunner struct {
	Timeout        time.Duration
	DetailsMaxSize int
}

func (r *ExecActorRunner) Run(ctx context.Context, req ActorRequest) ActorResult {
	start := time.Now()

	if len(req.Command) == 0 {
		return ActorResult{
			OK:        false,
			ErrorType: ErrorExec,
			Error:     "empty command",
			StartedAt: start,
			EndedAt:   time.Now(),
		}
	}

	path := req.Command[0]

	// ❗ exec pre-check (NO RETRY scenario)
	if _, err := os.Stat(path); err != nil {
		return ActorResult{
			OK:        false,
			ErrorType: ErrorExec,
			Error:     "file not accessible: " + err.Error(),
			StartedAt: start,
			EndedAt:   time.Now(),
		}
	}

	cctx, cancel := context.WithTimeout(ctx, r.Timeout)
	defer cancel()

	cmd := exec.CommandContext(cctx, path, req.Command[1:]...)

	cmd.Env = append(os.Environ(),
		"CT_ACTOR="+string(req.Name),
		"CT_CLUSTER_GROUP="+req.ClusterGroup,
		"CT_MANAGEMENT_GROUP="+req.ManagementGroup,
		"CT_NODE_ID="+req.NodeID,
		"CT_ROLE="+req.Role,
		"CT_DESIRED="+req.Desired,
	)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	res := ActorResult{
		StartedAt: start,
		EndedAt:   time.Now(),
	}
	res.Duration = res.EndedAt.Sub(start)

	res.Stdout = trim(stdout.String(), r.DetailsMaxSize)
	res.Stderr = trim(stderr.String(), r.DetailsMaxSize)

	// timeout
	if errors.Is(cctx.Err(), context.DeadlineExceeded) {
		res.OK = false
		res.ErrorType = ErrorTimeout
		res.Error = "timeout"
		res.ExitCode = -1
		return res
	}

	// exec failed AFTER start
	if err != nil {
		res.OK = false

		if ee, ok := err.(*exec.ExitError); ok {
			res.ErrorType = ErrorExitCode
			res.ExitCode = ee.ExitCode()
			res.Error = err.Error()
			return res
		}

		// critical exec failure (should NOT retry)
		res.ErrorType = ErrorExec
		res.ExitCode = -2
		res.Error = err.Error()
		return res
	}

	res.OK = true
	res.ExitCode = 0
	return res
}

func trim(s string, max int) string {
	if max <= 0 || len(s) <= max {
		return s
	}
	return s[:max]
}
