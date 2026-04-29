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

type CommandStatus string

const (
	CommandPending   CommandStatus = "pending"
	CommandRunning   CommandStatus = "running"
	CommandCompleted CommandStatus = "completed"
	CommandFailed    CommandStatus = "failed"
)

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

type DynamicConfigNameDocument struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type DynamicConfigDocument struct {
	Cluster       DynamicConfigNameDocument            `json:"cluster"`
	ClusterGroups map[string]DynamicConfigNameDocument `json:"cluster_groups"`
	Roles         map[string]DynamicConfigNameDocument `json:"roles"`
	Nodes         map[string]DynamicConfigNameDocument `json:"nodes"`
	UpdatedAt     time.Time                            `json:"updated_at"`
}
