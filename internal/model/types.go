// Package model defines domain types and etcd key path functions.
package model

import "time"

type DesiredState string

const (
	DesiredIdle    DesiredState = "idle"
	DesiredActive  DesiredState = "active"
	DesiredPassive DesiredState = "passive"
)

type ActualState string

const (
	ActualIdle     ActualState = "idle"
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

// CommandStatus tracks processing stages for a queued command.
// Producer (API) writes Pending; consumer (leader controller) advances the lifecycle.
// The consumer is not yet implemented.
type CommandStatus string

const (
	CommandPending   CommandStatus = "pending"
	CommandRunning   CommandStatus = "running"
	CommandCompleted CommandStatus = "completed"
	CommandFailed    CommandStatus = "failed"
)

// Command is the document written to commands/{id} by any node via POST /api/v1/commands.
// The leader is expected to read this queue, apply the requested desired-state change, and
// archive the document to commands_history/{id}. Queue processing is not yet implemented.
type Command struct {
	ID              string        `json:"id"`
	Type            string        `json:"type"`
	ClusterGroup    string        `json:"cluster_group"`
	ManagementGroup string        `json:"management_group"`
	Desired         DesiredState  `json:"desired"`
	Status          CommandStatus `json:"status"`
	Owner           string        `json:"owner,omitempty"`
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
