package roles

import "time"

type ActorName string

const (
	ProbeActive  ActorName = "probe_active"
	SetActive    ActorName = "set_active"
	ProbePassive ActorName = "probe_passive"
	SetPassive   ActorName = "set_passive"
	ForceStop    ActorName = "force_stop"
)

type ErrorType string

const (
	ErrorExec     ErrorType = "exec_error"
	ErrorExitCode ErrorType = "exit_code"
	ErrorTimeout  ErrorType = "timeout"
)

type ActorRequest struct {
	Name            ActorName
	Command         []string
	ClusterGroup    string
	ManagementGroup string
	NodeID          string
	Role            string
	Desired         string
}

type ActorResult struct {
	OK        bool
	ErrorType ErrorType
	ExitCode  int

	Stdout string
	Stderr string
	Error  string

	StartedAt time.Time
	EndedAt   time.Time
	Duration  time.Duration
}
