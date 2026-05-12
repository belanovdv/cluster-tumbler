// Package model defines domain types and etcd key path functions.
package model

import "time"

type DesiredState string

const (
	DesiredActive  DesiredState = "active"
	DesiredPassive DesiredState = "passive"
)

type ActualState string

const (
	ActualActive   ActualState = "active"
	ActualPassive  ActualState = "passive"
	ActualStarting ActualState = "starting"
	ActualStopping ActualState = "stopping"
	ActualFailed   ActualState = "failed"
)

type HealthStatus string

const (
	HealthOK      HealthStatus = "ok"
	HealthWarning HealthStatus = "warning"
	HealthFailed  HealthStatus = "failed"
)

type DesiredDocument struct {
	State     DesiredState `json:"state"`
	Managed   bool         `json:"managed,omitempty"`
	UpdatedAt time.Time    `json:"updated_at"`
	Details   string       `json:"details,omitempty"`
}

type ActualDocument struct {
	State     ActualState `json:"state"`
	UpdatedAt time.Time   `json:"updated_at"`
	Details   string      `json:"details,omitempty"`
}

type HealthDocument struct {
	Status    HealthStatus `json:"status"`
	UpdatedAt time.Time    `json:"updated_at"`
	Details   string       `json:"details,omitempty"`
}

type ManagementGroupConfigDocument struct {
	Priority  int       `json:"priority"`
	Roles     []string  `json:"roles,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`
}

type MembershipDocument struct {
	ClusterGroup    string   `json:"cluster_group"`
	ManagementGroup string   `json:"management_group"`
	Priority        int      `json:"priority"`
	Roles           []string `json:"roles"`
}

type RegistrationDocument struct {
	NodeID      string               `json:"node_id"`
	Memberships []MembershipDocument `json:"memberships"`
	UpdatedAt   time.Time            `json:"updated_at"`
}

type SessionDocument struct {
	NodeID    string    `json:"node_id"`
	UpdatedAt time.Time `json:"updated_at"`
}

type LeadershipDocument struct {
	OwnerNodeID string    `json:"owner_node_id"`
	LeaseID     int64     `json:"lease_id"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// CommandType identifies the kind of management command issued via POST /api/v1/commands.
type CommandType string

const (
	CommandTypePromote      CommandType = "promote"       // swap priorities so target becomes highest-priority
	CommandTypeDisable      CommandType = "disable"       // set managed=false, preserving current desired state
	CommandTypeEnable       CommandType = "enable"        // set managed=true, preserving current desired state
	CommandTypeReload       CommandType = "reload"        // clear failed state, attempt passive convergence
	CommandTypeForcePassive CommandType = "force_passive" // force-stop services on a group with managed=false
)

// CommandStatus tracks the lifecycle stages of a queued command.
type CommandStatus string

const (
	CommandPending   CommandStatus = "pending"
	CommandRunning   CommandStatus = "running"
	CommandCompleted CommandStatus = "completed"
	CommandFailed    CommandStatus = "failed"
)

// Command is written to commands/{id} by POST /api/v1/commands and processed by the leader consumer.
type Command struct {
	ID              string        `json:"id"`
	Type            CommandType   `json:"type"`
	ClusterGroup    string        `json:"cluster_group"`
	ManagementGroup string        `json:"management_group"`
	Status          CommandStatus `json:"status"`
	Error           string        `json:"error,omitempty"`
	CreatedAt       time.Time     `json:"created_at"`
	StartedAt       *time.Time    `json:"started_at,omitempty"`
	FinishedAt      *time.Time    `json:"finished_at,omitempty"`
}

// ClusterConfigDocument is stored at config/_meta.
type ClusterConfigDocument struct {
	ID                  string    `json:"id"`
	Name                string    `json:"name"`
	FailoverMode        string    `json:"failover_mode,omitempty"`
	LeaderTTL           string    `json:"leader_ttl,omitempty"`
	LeaderRenewInterval string    `json:"leader_renew_interval,omitempty"`
	SessionTTL          string    `json:"session_ttl,omitempty"`
	UpdatedAt           time.Time `json:"updated_at"`
}

// ClusterGroupConfigDocument is stored at config/cluster_groups/{group_id}.
type ClusterGroupConfigDocument struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	UpdatedAt time.Time `json:"updated_at"`
}

type RoleTimeoutsDocument struct {
	Exec           string `json:"exec,omitempty"`
	Converge       string `json:"converge,omitempty"`
	RetryInterval  string `json:"retry_interval,omitempty"`
	CheckInterval  string `json:"check_interval,omitempty"`
	DetailsMaxSize int    `json:"details_max_size,omitempty"`
}

// RoleConfigDocument is stored at config/roles/{role_id}.
type RoleConfigDocument struct {
	ID        string              `json:"id"`
	Name      string              `json:"name"`
	Actors    map[string][]string `json:"actors,omitempty"`
	Timeouts  RoleTimeoutsDocument `json:"timeouts"`
	UpdatedAt time.Time           `json:"updated_at"`
}

type MembershipRef struct {
	ClusterGroup    string `json:"cluster_group"`
	ManagementGroup string `json:"management_group"`
}

// NodeConfigDocument is stored at config/nodes/{node_id}.
type NodeConfigDocument struct {
	ID          string          `json:"id"`
	Name        string          `json:"name"`
	Memberships []MembershipRef `json:"memberships,omitempty"`
	UpdatedAt   time.Time       `json:"updated_at"`
}
