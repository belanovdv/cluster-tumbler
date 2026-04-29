package config

import (
	"errors"
	"fmt"
	"os"
	"time"

	"cluster-tumbler/internal/logging"

	"gopkg.in/yaml.v3"
)

type Duration struct {
	time.Duration
}

func (d *Duration) UnmarshalYAML(value *yaml.Node) error {
	var raw string
	if err := value.Decode(&raw); err != nil {
		return err
	}

	parsed, err := time.ParseDuration(raw)
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", raw, err)
	}

	d.Duration = parsed
	return nil
}

type Config struct {
	Local   LocalConfig           `yaml:"local"`
	Cluster ClusterConfig         `yaml:"cluster"`
	Agent   AgentConfig           `yaml:"agent"`
	Roles   map[string]RoleConfig `yaml:"roles"`
}

type LocalConfig struct {
	Etcd   EtcdConfig     `yaml:"etcd"`
	Logger logging.Config `yaml:"logger"`
	API    APIConfig      `yaml:"api"`
}

type EtcdConfig struct {
	Endpoints     []string `yaml:"endpoints"`
	DialTimeout   Duration `yaml:"dial_timeout"`
	RetryInterval Duration `yaml:"retry_interval"`
}

type APIConfig struct {
	Listen string `yaml:"listen"`
}

type ClusterConfig struct {
	ID                  string                        `yaml:"id"`
	Name                string                        `yaml:"name"`
	Groups              map[string]ClusterGroupConfig `yaml:"groups"`
	FailoverMode        string                        `yaml:"failover_mode"`
	LeaderTTL           Duration                      `yaml:"leader_ttl"`
	LeaderRenewInterval Duration                      `yaml:"leader_renew_interval"`
	SessionTTL          Duration                      `yaml:"session_ttl"`
}

type ClusterGroupConfig struct {
	Name string `yaml:"name"`
}

type AgentConfig struct {
	NodeID      string             `yaml:"node_id"`
	Name        string             `yaml:"name"`
	Memberships []MembershipConfig `yaml:"memberships"`
}

type MembershipConfig struct {
	ClusterGroup    string   `yaml:"cluster_group"`
	ManagementGroup string   `yaml:"management_group"`
	Priority        int      `yaml:"priority"`
	Roles           []string `yaml:"roles"`
}

type RoleConfig struct {
	Name     string       `yaml:"name"`
	Actors   RoleActors   `yaml:"actors"`
	Timeouts RoleTimeouts `yaml:"timeouts"`
}

// RoleActors maps actor name to command argv.
// Example:
//
//	actors:
//	  probe_active:
//	    - ./test/testdata/scripts/probe_active.sh
//	    - pg
type RoleActors map[string][]string

type RoleTimeouts struct {
	Exec           Duration `yaml:"exec"`
	Converge       Duration `yaml:"converge"`
	RetryInterval  Duration `yaml:"retry_interval"`
	CheckInterval  Duration `yaml:"check_interval"`
	DetailsMaxSize int      `yaml:"details_max_size"`
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}

	applyDefaults(&cfg)

	if err := validate(&cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}

func applyDefaults(cfg *Config) {
	if cfg.Local.Logger.Level == "" {
		cfg.Local.Logger.Level = "debug"
	}

	if cfg.Local.Logger.Format == "" {
		cfg.Local.Logger.Format = "plain"
	}

	if cfg.Local.API.Listen == "" {
		cfg.Local.API.Listen = ":5080"
	}

	if cfg.Local.Etcd.DialTimeout.Duration == 0 {
		cfg.Local.Etcd.DialTimeout.Duration = 3 * time.Second
	}

	if cfg.Local.Etcd.RetryInterval.Duration == 0 {
		cfg.Local.Etcd.RetryInterval.Duration = time.Second
	}

	if cfg.Cluster.Name == "" {
		cfg.Cluster.Name = cfg.Cluster.ID
	}

	for groupID, group := range cfg.Cluster.Groups {
		if group.Name == "" {
			group.Name = groupID
		}
		cfg.Cluster.Groups[groupID] = group
	}

	if cfg.Agent.Name == "" {
		cfg.Agent.Name = cfg.Agent.NodeID
	}

	if cfg.Cluster.FailoverMode == "" {
		cfg.Cluster.FailoverMode = "manual"
	}

	if cfg.Cluster.LeaderTTL.Duration == 0 {
		cfg.Cluster.LeaderTTL.Duration = 2 * time.Second
	}

	if cfg.Cluster.LeaderRenewInterval.Duration == 0 {
		cfg.Cluster.LeaderRenewInterval.Duration = 500 * time.Millisecond
	}

	if cfg.Cluster.SessionTTL.Duration == 0 {
		cfg.Cluster.SessionTTL.Duration = 30 * time.Second
	}

	for roleName, role := range cfg.Roles {
		if role.Name == "" {
			role.Name = roleName
		}

		if role.Timeouts.Exec.Duration == 0 {
			role.Timeouts.Exec.Duration = 5 * time.Second
		}

		if role.Timeouts.Converge.Duration == 0 {
			role.Timeouts.Converge.Duration = 10 * time.Second
		}

		if role.Timeouts.RetryInterval.Duration == 0 {
			role.Timeouts.RetryInterval.Duration = time.Second
		}

		if role.Timeouts.CheckInterval.Duration == 0 {
			role.Timeouts.CheckInterval.Duration = 5 * time.Second
		}

		if role.Timeouts.DetailsMaxSize == 0 {
			role.Timeouts.DetailsMaxSize = 4096
		}

		cfg.Roles[roleName] = role
	}
}

func validate(cfg *Config) error {
	if cfg.Cluster.ID == "" {
		return errors.New("cluster.id is required")
	}

	if len(cfg.Cluster.Groups) == 0 {
		return errors.New("cluster.groups must contain at least one group")
	}

	if len(cfg.Local.Etcd.Endpoints) == 0 {
		return errors.New("local.etcd.endpoints must contain at least one endpoint")
	}

	if cfg.Agent.NodeID == "" {
		return errors.New("agent.node_id is required")
	}

	if len(cfg.Agent.Memberships) == 0 {
		return errors.New("agent.memberships must contain at least one membership")
	}

	groupSet := make(map[string]struct{}, len(cfg.Cluster.Groups))
	for groupID := range cfg.Cluster.Groups {
		if groupID == "" {
			return errors.New("cluster.groups contains empty group id")
		}
		groupSet[groupID] = struct{}{}
	}

	for _, membership := range cfg.Agent.Memberships {
		if _, ok := groupSet[membership.ClusterGroup]; !ok {
			return fmt.Errorf(
				"membership cluster_group %q is not declared in cluster.groups",
				membership.ClusterGroup,
			)
		}

		if membership.ManagementGroup == "" {
			return errors.New("membership.management_group is required")
		}

		if membership.Priority <= 0 {
			return fmt.Errorf(
				"membership %s/%s priority must be greater than zero",
				membership.ClusterGroup,
				membership.ManagementGroup,
			)
		}

		if len(membership.Roles) == 0 {
			return fmt.Errorf(
				"membership %s/%s must contain at least one role",
				membership.ClusterGroup,
				membership.ManagementGroup,
			)
		}

		for _, roleName := range membership.Roles {
			if _, ok := cfg.Roles[roleName]; !ok {
				return fmt.Errorf("membership references undefined role %q", roleName)
			}
		}
	}

	for roleName, role := range cfg.Roles {
		for actorName, command := range role.Actors {
			if actorName == "" {
				return fmt.Errorf("role %q has empty actor name", roleName)
			}

			if len(command) == 0 {
				return fmt.Errorf(
					"role %q actor %q command must not be empty",
					roleName,
					actorName,
				)
			}

			if command[0] == "" {
				return fmt.Errorf(
					"role %q actor %q executable path must not be empty",
					roleName,
					actorName,
				)
			}
		}
	}

	return nil
}
