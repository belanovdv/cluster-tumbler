// Package config loads and validates the agent YAML config and merges etcd overrides.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"cluster-tumbler/internal/logging"
	"cluster-tumbler/internal/model"

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
	Etcd             EtcdConfig                                   `yaml:"etcd"`
	Logger           logging.Config                               `yaml:"logger"`
	API              APIConfig                                    `yaml:"api"`
	Cluster          ClusterConfig                                `yaml:"cluster"`
	Node             NodeConfig                                   `yaml:"node"`
	ManagementGroups map[string]map[string]ManagementGroupConfig  `yaml:"management_groups"`
	Roles            RolesMap                                     `yaml:"roles"`
}

type EtcdConfig struct {
	Endpoints     []string `yaml:"endpoints"`
	DialTimeout   Duration `yaml:"dial_timeout"`
	RetryInterval Duration `yaml:"retry_interval"`
}

type APIConfig struct {
	Listen string `yaml:"listen"`
	Token  string `yaml:"token"`
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

type NodeConfig struct {
	NodeID             string             `yaml:"node_id"`
	Name               string             `yaml:"name"`
	ActorsBaseDir      string             `yaml:"actors_base_dir"`
	DisableAPI         bool               `yaml:"disable_api"`
	DisableController  bool               `yaml:"disable_controller"`
	Memberships        []MembershipConfig `yaml:"memberships"`
}

type MembershipConfig struct {
	ClusterGroup    string `yaml:"cluster_group"`
	ManagementGroup string `yaml:"management_group"`
}

type ManagementGroupConfig struct {
	Priority int      `yaml:"priority"`
	Roles    []string `yaml:"roles"`
}

type RoleConfig struct {
	Name     string       `yaml:"name"`
	Actors   RoleActors   `yaml:"actors"`
	Timeouts RoleTimeouts `yaml:"timeouts"`
}

// ActorCommand is an argv list for an actor executable. Accepts both a YAML
// string ("./script.sh arg") and a YAML sequence (["./script.sh", "arg"]).
type ActorCommand []string

func (a *ActorCommand) UnmarshalYAML(value *yaml.Node) error {
	switch value.Kind {
	case yaml.ScalarNode:
		var raw string
		if err := value.Decode(&raw); err != nil {
			return err
		}
		*a = strings.Fields(raw)
		return nil
	case yaml.SequenceNode:
		var list []string
		if err := value.Decode(&list); err != nil {
			return err
		}
		*a = list
		return nil
	}
	return fmt.Errorf("actor command must be a string or a list")
}

// RoleActors maps actor name to command argv.
type RoleActors map[string]ActorCommand

// RolesMap is a map of role configs that supports a reserved "defaults" key in YAML.
// When "defaults" is present it is applied as a base to every other role entry,
// with per-role values taking precedence over defaults (zero value = inherit).
type RolesMap map[string]RoleConfig

// UnmarshalYAML extracts the optional "defaults" key and applies it as a base to every role entry.
func (m *RolesMap) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind != yaml.MappingNode {
		return fmt.Errorf("roles must be a YAML mapping")
	}

	raw := make(map[string]*yaml.Node, len(value.Content)/2)
	for i := 0; i+1 < len(value.Content); i += 2 {
		raw[value.Content[i].Value] = value.Content[i+1]
	}

	var defaults RoleConfig
	if node, ok := raw["defaults"]; ok {
		if err := node.Decode(&defaults); err != nil {
			return fmt.Errorf("roles.defaults: %w", err)
		}
	}

	result := make(RolesMap, len(raw))
	for key, node := range raw {
		if key == "defaults" {
			continue
		}
		var role RoleConfig
		if err := node.Decode(&role); err != nil {
			return fmt.Errorf("roles.%s: %w", key, err)
		}
		result[key] = applyRoleDefaults(role, defaults)
	}

	*m = result
	return nil
}

// applyRoleDefaults fills zero-value fields in role from def (actors map and each timeout individually).
func applyRoleDefaults(role, def RoleConfig) RoleConfig {
	if len(role.Actors) == 0 && len(def.Actors) > 0 {
		actors := make(RoleActors, len(def.Actors))
		for k, v := range def.Actors {
			cmd := make(ActorCommand, len(v))
			copy(cmd, v)
			actors[k] = cmd
		}
		role.Actors = actors
	}
	if role.Timeouts.Exec.Duration == 0 {
		role.Timeouts.Exec = def.Timeouts.Exec
	}
	if role.Timeouts.Converge.Duration == 0 {
		role.Timeouts.Converge = def.Timeouts.Converge
	}
	if role.Timeouts.RetryInterval.Duration == 0 {
		role.Timeouts.RetryInterval = def.Timeouts.RetryInterval
	}
	if role.Timeouts.CheckInterval.Duration == 0 {
		role.Timeouts.CheckInterval = def.Timeouts.CheckInterval
	}
	if role.Timeouts.DetailsMaxSize == 0 {
		role.Timeouts.DetailsMaxSize = def.Timeouts.DetailsMaxSize
	}
	return role
}

type RoleTimeouts struct {
	Exec           Duration `yaml:"exec"`
	Converge       Duration `yaml:"converge"`
	RetryInterval  Duration `yaml:"retry_interval"`
	CheckInterval  Duration `yaml:"check_interval"`
	DetailsMaxSize int      `yaml:"details_max_size"`
}

// EtcdSnapshot holds config documents loaded from etcd for use in Merge.
type EtcdSnapshot struct {
	Cluster          *model.ClusterConfigDocument
	ClusterGroups    map[string]*model.ClusterGroupConfigDocument
	Roles            map[string]*model.RoleConfigDocument
	ManagementGroups map[string]map[string]*model.ManagementGroupConfigDocument
	Node             *model.NodeConfigDocument
}

// Merge returns a new *Config with etcd values overriding local where non-empty.
// Node-local fields (etcd endpoints, api listen, logger, node_id, actors_base_dir)
// are always taken from local.
func Merge(local *Config, snap *EtcdSnapshot) *Config {
	if snap == nil {
		return local
	}

	merged := *local

	// Deep copy Cluster (groups map)
	merged.Cluster.Groups = make(map[string]ClusterGroupConfig, len(local.Cluster.Groups))
	for k, v := range local.Cluster.Groups {
		merged.Cluster.Groups[k] = v
	}

	// Deep copy Roles
	merged.Roles = make(RolesMap, len(local.Roles))
	for k, v := range local.Roles {
		actors := make(RoleActors, len(v.Actors))
		for ak, av := range v.Actors {
			cmd := make(ActorCommand, len(av))
			copy(cmd, av)
			actors[ak] = cmd
		}
		v.Actors = actors
		merged.Roles[k] = v
	}

	// Deep copy ManagementGroups
	merged.ManagementGroups = make(map[string]map[string]ManagementGroupConfig, len(local.ManagementGroups))
	for cg, mgMap := range local.ManagementGroups {
		merged.ManagementGroups[cg] = make(map[string]ManagementGroupConfig, len(mgMap))
		for mg, mgCfg := range mgMap {
			roles := make([]string, len(mgCfg.Roles))
			copy(roles, mgCfg.Roles)
			mgCfg.Roles = roles
			merged.ManagementGroups[cg][mg] = mgCfg
		}
	}

	// Deep copy Node.Memberships
	merged.Node.Memberships = make([]MembershipConfig, len(local.Node.Memberships))
	copy(merged.Node.Memberships, local.Node.Memberships)

	// Apply cluster config from etcd
	if c := snap.Cluster; c != nil {
		if c.Name != "" {
			merged.Cluster.Name = c.Name
		}
		if c.FailoverMode != "" {
			merged.Cluster.FailoverMode = c.FailoverMode
		}
		if d, err := time.ParseDuration(c.LeaderTTL); err == nil && d > 0 {
			merged.Cluster.LeaderTTL = Duration{Duration: d}
		}
		if d, err := time.ParseDuration(c.LeaderRenewInterval); err == nil && d > 0 {
			merged.Cluster.LeaderRenewInterval = Duration{Duration: d}
		}
		if d, err := time.ParseDuration(c.SessionTTL); err == nil && d > 0 {
			merged.Cluster.SessionTTL = Duration{Duration: d}
		}
	}

	// Apply cluster groups from etcd
	for id, groupDoc := range snap.ClusterGroups {
		if groupDoc.Name == "" {
			continue
		}
		g := merged.Cluster.Groups[id]
		g.Name = groupDoc.Name
		merged.Cluster.Groups[id] = g
	}

	// Apply roles from etcd
	for id, roleDoc := range snap.Roles {
		role := merged.Roles[id]
		if roleDoc.Name != "" {
			role.Name = roleDoc.Name
		}
		if len(roleDoc.Actors) > 0 {
			role.Actors = make(RoleActors, len(roleDoc.Actors))
			for k, v := range roleDoc.Actors {
				role.Actors[k] = ActorCommand(v)
			}
		}
		t := roleDoc.Timeouts
		if d, err := time.ParseDuration(t.Exec); err == nil && d > 0 {
			role.Timeouts.Exec = Duration{Duration: d}
		}
		if d, err := time.ParseDuration(t.Converge); err == nil && d > 0 {
			role.Timeouts.Converge = Duration{Duration: d}
		}
		if d, err := time.ParseDuration(t.RetryInterval); err == nil && d > 0 {
			role.Timeouts.RetryInterval = Duration{Duration: d}
		}
		if d, err := time.ParseDuration(t.CheckInterval); err == nil && d > 0 {
			role.Timeouts.CheckInterval = Duration{Duration: d}
		}
		if t.DetailsMaxSize > 0 {
			role.Timeouts.DetailsMaxSize = t.DetailsMaxSize
		}
		merged.Roles[id] = role
	}

	// Apply management groups from etcd
	for cg, mgMap := range snap.ManagementGroups {
		if merged.ManagementGroups[cg] == nil {
			merged.ManagementGroups[cg] = make(map[string]ManagementGroupConfig)
		}
		for mg, mgDoc := range mgMap {
			mgCfg := merged.ManagementGroups[cg][mg]
			if mgDoc.Priority > 0 {
				mgCfg.Priority = mgDoc.Priority
			}
			if len(mgDoc.Roles) > 0 {
				mgCfg.Roles = mgDoc.Roles
			}
			merged.ManagementGroups[cg][mg] = mgCfg
		}
	}

	// Apply this node's config from etcd
	if n := snap.Node; n != nil {
		if n.Name != "" {
			merged.Node.Name = n.Name
		}
		if len(n.Memberships) > 0 {
			merged.Node.Memberships = make([]MembershipConfig, len(n.Memberships))
			for i, m := range n.Memberships {
				merged.Node.Memberships[i] = MembershipConfig{
					ClusterGroup:    m.ClusterGroup,
					ManagementGroup: m.ManagementGroup,
				}
			}
		}
	}

	return &merged
}

// ResolveActorPath resolves an actor executable path against Node.ActorsBaseDir.
// Absolute paths are returned unchanged. Relative paths are joined with the base.
// If ActorsBaseDir is empty, path is returned unchanged (relative to CWD).
func (cfg *Config) ResolveActorPath(execPath string) string {
	if cfg.Node.ActorsBaseDir == "" || filepath.IsAbs(execPath) {
		return execPath
	}
	return filepath.Join(cfg.Node.ActorsBaseDir, execPath)
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

// applyDefaults fills in zero-value config fields with hardcoded defaults before validation.
func applyDefaults(cfg *Config) {
	if cfg.Logger.Level == "" {
		cfg.Logger.Level = "debug"
	}

	if cfg.Logger.Format == "" {
		cfg.Logger.Format = "plain"
	}

	if cfg.API.Listen == "" {
		cfg.API.Listen = ":5080"
	}

	if cfg.Etcd.DialTimeout.Duration == 0 {
		cfg.Etcd.DialTimeout.Duration = 3 * time.Second
	}

	if cfg.Etcd.RetryInterval.Duration == 0 {
		cfg.Etcd.RetryInterval.Duration = time.Second
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

	if cfg.Node.Name == "" {
		cfg.Node.Name = cfg.Node.NodeID
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

// validate checks required fields and cross-reference consistency (memberships → groups → roles).
func validate(cfg *Config) error {
	if cfg.Cluster.ID == "" {
		return errors.New("cluster.id is required")
	}

	if len(cfg.Cluster.Groups) == 0 {
		return errors.New("cluster.groups must contain at least one group")
	}

	if len(cfg.Etcd.Endpoints) == 0 {
		return errors.New("etcd.endpoints must contain at least one endpoint")
	}

	if cfg.Node.NodeID == "" {
		return errors.New("node.node_id is required")
	}

	if len(cfg.Node.Memberships) == 0 {
		return errors.New("node.memberships must contain at least one membership")
	}

	groupSet := make(map[string]struct{}, len(cfg.Cluster.Groups))
	for groupID := range cfg.Cluster.Groups {
		if groupID == "" {
			return errors.New("cluster.groups contains empty group id")
		}
		groupSet[groupID] = struct{}{}
	}

	for _, membership := range cfg.Node.Memberships {
		if _, ok := groupSet[membership.ClusterGroup]; !ok {
			return fmt.Errorf(
				"membership cluster_group %q is not declared in cluster.groups",
				membership.ClusterGroup,
			)
		}

		if membership.ManagementGroup == "" {
			return errors.New("membership.management_group is required")
		}

		mgGroups, ok := cfg.ManagementGroups[membership.ClusterGroup]
		if !ok {
			return fmt.Errorf(
				"membership cluster_group %q has no entry in management_groups",
				membership.ClusterGroup,
			)
		}

		mgCfg, ok := mgGroups[membership.ManagementGroup]
		if !ok {
			return fmt.Errorf(
				"membership management_group %q/%q is not declared in management_groups",
				membership.ClusterGroup,
				membership.ManagementGroup,
			)
		}

		if mgCfg.Priority <= 0 {
			return fmt.Errorf(
				"management_groups %s/%s priority must be greater than zero",
				membership.ClusterGroup,
				membership.ManagementGroup,
			)
		}

		if len(mgCfg.Roles) == 0 {
			return fmt.Errorf(
				"management_groups %s/%s must contain at least one role",
				membership.ClusterGroup,
				membership.ManagementGroup,
			)
		}

		for _, roleName := range mgCfg.Roles {
			if _, ok := cfg.Roles[roleName]; !ok {
				return fmt.Errorf("management_group %s/%s references undefined role %q",
					membership.ClusterGroup,
					membership.ManagementGroup,
					roleName,
				)
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
