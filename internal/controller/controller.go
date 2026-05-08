// Package controller implements the leader-only reconciliation loop for cluster state.
package controller

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"

	"cluster-tumbler/internal/config"
	"cluster-tumbler/internal/etcd"
	"cluster-tumbler/internal/model"
	"cluster-tumbler/internal/store"

	"go.uber.org/zap"
)

type Controller struct {
	cfg   *config.Config
	store *store.StateStore
	etcd  *etcd.Client
	log   *zap.Logger

	lastManagementGroups map[string][]string
	switchoverStarted    map[string]time.Time  // key: "clusterGroup/mgName" → when phase-1 began
	switchoverTarget     map[string]string     // key: clusterGroup → target MG that phase-1 was initiated for
}

type groupRuntime struct {
	ClusterGroup    string
	ManagementGroup string
	Priority        int
	Desired         model.DesiredState
	Actual          model.ActualState
	Health          model.HealthStatus
	Available       bool
}

type expectedRoleState struct {
	NodeID string
	Role   string
}

func New(cfg *config.Config, st *store.StateStore, etcdClient *etcd.Client, log *zap.Logger) *Controller {
	return &Controller{
		cfg:                  cfg,
		store:                st,
		etcd:                 etcdClient,
		log:                  log,
		lastManagementGroups: make(map[string][]string),
		switchoverStarted:    make(map[string]time.Time),
		switchoverTarget:     make(map[string]string),
	}
}

func (c *Controller) Run(ctx context.Context) error {
	c.log.Debug("starting controller loop")

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	if err := c.Reconcile(ctx); err != nil {
		c.log.Error("initial controller reconcile failed", zap.Error(err))
	}

	for {
		select {
		case <-ticker.C:
			if err := c.Reconcile(ctx); err != nil {
				c.log.Error("controller reconcile failed", zap.Error(err))
			}

		case <-ctx.Done():
			c.log.Debug("controller stopped")
			return ctx.Err()
		}
	}
}

// Reconcile is the entry point for one reconciliation cycle: iterates every management group and applies policy.
func (c *Controller) Reconcile(ctx context.Context) error {
	if !c.store.Ready() {
		c.log.Debug("state store is not ready, skip reconcile")
		return nil
	}

	for clusterGroup := range c.cfg.Cluster.Groups {
		groupPrefix := model.ClusterGroup(c.cfg.Cluster.ID, clusterGroup)
		managementGroups := c.store.ListChildren(groupPrefix)

		if !reflect.DeepEqual(c.lastManagementGroups[clusterGroup], managementGroups) {
			c.log.Debug(
				"discovered management groups changed",
				zap.String("cluster_group", clusterGroup),
				zap.Strings("management_groups", managementGroups),
			)

			c.lastManagementGroups[clusterGroup] = append([]string(nil), managementGroups...)
		}

		runtimes := make([]groupRuntime, 0, len(managementGroups))

		for _, managementGroup := range managementGroups {
			if isSystemKey(managementGroup) {
				continue
			}

			runtime, err := c.reconcileManagementGroup(ctx, clusterGroup, managementGroup)
			if err != nil {
				c.log.Error(
					"management group reconcile failed",
					zap.String("cluster_group", clusterGroup),
					zap.String("management_group", managementGroup),
					zap.Error(err),
				)
				continue
			}

			runtimes = append(runtimes, runtime)
		}

		if err := c.applyPriorityPolicy(ctx, clusterGroup, runtimes); err != nil {
			c.log.Error(
				"priority policy failed",
				zap.String("cluster_group", clusterGroup),
				zap.Error(err),
			)
		}
	}

	return nil
}

// reconcileManagementGroup aggregates per-role actual/health into a group-level state and writes if changed.
func (c *Controller) reconcileManagementGroup(
	ctx context.Context,
	clusterGroup string,
	managementGroup string,
) (groupRuntime, error) {
	prefix := model.ManagementGroup(c.cfg.Cluster.ID, clusterGroup, managementGroup)
	items := c.store.Prefix(prefix)
	now := time.Now().UTC()

	desired := c.readDesired(clusterGroup, managementGroup)

	var (
		seenRoles    bool
		seenActive   bool
		seenPassive  bool
		seenStarting bool
		seenStopping bool
		seenIdle     bool

		latestStartingAt time.Time
		latestStoppingAt time.Time
	)

	actualState := model.ActualIdle
	healthStatus := model.HealthWarning
	details := "empty management group"

	if desired != model.DesiredIdle {
		missing := c.missingExpectedRoleStates(clusterGroup, managementGroup)
		if len(missing) > 0 {
			actualState = model.ActualFailed
			healthStatus = model.HealthFailed
			details = "agent lost"

			c.log.Debug(
				"detected missing expected role state",
				zap.String("cluster_group", clusterGroup),
				zap.String("management_group", managementGroup),
				zap.Strings("missing", missing),
			)
		}
	}

	if actualState != model.ActualFailed {
		for key, value := range items {
			if !strings.HasSuffix(key, "/actual") {
				continue
			}

			if key == model.Actual(c.cfg.Cluster.ID, clusterGroup, managementGroup) {
				continue
			}

			var actual model.ActualDocument
			if err := json.Unmarshal(value, &actual); err != nil {
				c.log.Debug("failed to decode role actual", zap.String("key", key), zap.Error(err))
				continue
			}

			seenRoles = true

			switch actual.State {
			case model.ActualFailed:
				actualState = model.ActualFailed
				healthStatus = model.HealthFailed
				details = "one or more roles are failed"

			case model.ActualActive:
				seenActive = true

			case model.ActualPassive:
				seenPassive = true

			case model.ActualStarting:
				seenStarting = true
				if actual.UpdatedAt.After(latestStartingAt) {
					latestStartingAt = actual.UpdatedAt
				}

			case model.ActualStopping:
				seenStopping = true
				if actual.UpdatedAt.After(latestStoppingAt) {
					latestStoppingAt = actual.UpdatedAt
				}

			case model.ActualIdle:
				seenIdle = true
			}

			if actualState == model.ActualFailed {
				break
			}
		}
	}

	if actualState != model.ActualFailed {
		switch {
		case !seenRoles:
			actualState = model.ActualIdle
			healthStatus = model.HealthWarning
			details = "empty management group"

		case seenStarting || seenStopping:
			healthStatus = model.HealthWarning
			if seenStarting && seenStopping {
				if latestStartingAt.After(latestStoppingAt) {
					actualState = model.ActualStarting
					details = "one or more roles are starting"
				} else {
					actualState = model.ActualStopping
					details = "one or more roles are stopping"
				}
			} else if seenStarting {
				actualState = model.ActualStarting
				details = "one or more roles are starting"
			} else {
				actualState = model.ActualStopping
				details = "one or more roles are stopping"
			}

		case seenIdle:
			actualState = model.ActualIdle
			healthStatus = model.HealthWarning
			details = "one or more roles are not stable"

		case seenActive && !seenPassive:
			actualState = model.ActualActive
			healthStatus = model.HealthOK
			details = ""

		case seenPassive && !seenActive:
			actualState = model.ActualPassive
			healthStatus = model.HealthOK
			details = ""

		case seenActive && seenPassive:
			actualState = model.ActualFailed
			healthStatus = model.HealthFailed
			details = "mixed active and passive roles"
		}
	}

	actualKey := model.Actual(c.cfg.Cluster.ID, clusterGroup, managementGroup)
	healthKey := model.Health(c.cfg.Cluster.ID, clusterGroup, managementGroup)

	actualChanged, actualData, err := c.buildActualIfChanged(actualKey, actualState, details, now)
	if err != nil {
		return groupRuntime{}, err
	}

	healthChanged, healthData, err := c.buildHealthIfChanged(healthKey, healthStatus, details, now)
	if err != nil {
		return groupRuntime{}, err
	}

	if actualChanged {
		c.log.Debug(
			"writing management group actual",
			zap.String("cluster_group", clusterGroup),
			zap.String("management_group", managementGroup),
			zap.String("actual", string(actualState)),
		)

		if err := c.etcd.Put(ctx, actualKey, actualData); err != nil {
			return groupRuntime{}, err
		}
	}

	if healthChanged {
		c.log.Debug(
			"writing management group health",
			zap.String("cluster_group", clusterGroup),
			zap.String("management_group", managementGroup),
			zap.String("health", string(healthStatus)),
		)

		if err := c.etcd.Put(ctx, healthKey, healthData); err != nil {
			return groupRuntime{}, err
		}
	}

	priority := c.readPriority(clusterGroup, managementGroup)

	return groupRuntime{
		ClusterGroup:    clusterGroup,
		ManagementGroup: managementGroup,
		Priority:        priority,
		Desired:         desired,
		Actual:          actualState,
		Health:          healthStatus,
		Available:       healthStatus != model.HealthFailed && actualState != model.ActualFailed,
	}, nil
}

func (c *Controller) readDesired(clusterGroup string, managementGroup string) model.DesiredState {
	key := model.Desired(c.cfg.Cluster.ID, clusterGroup, managementGroup)

	raw, ok := c.store.Get(key)
	if !ok {
		return model.DesiredIdle
	}

	var doc model.DesiredDocument
	if err := json.Unmarshal(raw, &doc); err != nil {
		return model.DesiredIdle
	}

	return doc.State
}

// missingExpectedRoleStates returns keys for roles that should have actual/health but do not (agent lost).
func (c *Controller) missingExpectedRoleStates(
	clusterGroup string,
	managementGroup string,
) []string {
	expected := c.expectedRoleStates(clusterGroup, managementGroup)
	if len(expected) == 0 {
		return nil
	}

	missing := make([]string, 0)

	for _, item := range expected {
		actualKey := model.RoleActual(
			c.cfg.Cluster.ID,
			clusterGroup,
			managementGroup,
			item.NodeID,
			item.Role,
		)

		healthKey := model.RoleHealth(
			c.cfg.Cluster.ID,
			clusterGroup,
			managementGroup,
			item.NodeID,
			item.Role,
		)

		if _, ok := c.store.Get(actualKey); !ok {
			missing = append(missing, item.NodeID+"/"+item.Role+"/actual")
			continue
		}

		if _, ok := c.store.Get(healthKey); !ok {
			missing = append(missing, item.NodeID+"/"+item.Role+"/health")
			continue
		}
	}

	sort.Strings(missing)

	return missing
}

// expectedRoleStates builds the list of (node, role) pairs expected in a group from the registry.
func (c *Controller) expectedRoleStates(
	clusterGroup string,
	managementGroup string,
) []expectedRoleState {
	registryItems := c.store.Prefix(model.RegistryRoot(c.cfg.Cluster.ID))
	if len(registryItems) == 0 {
		return nil
	}

	out := make([]expectedRoleState, 0)

	for key, raw := range registryItems {
		if strings.Contains(strings.TrimPrefix(key, model.RegistryRoot(c.cfg.Cluster.ID)+"/"), "/") {
			continue
		}

		var reg model.RegistrationDocument
		if err := json.Unmarshal(raw, &reg); err != nil {
			c.log.Debug("failed to decode registration", zap.String("key", key), zap.Error(err))
			continue
		}

		nodeID := reg.NodeID
		if nodeID == "" {
			nodeID = strings.TrimPrefix(key, model.RegistryRoot(c.cfg.Cluster.ID)+"/")
		}

		for _, membership := range reg.Memberships {
			if membership.ClusterGroup != clusterGroup {
				continue
			}

			if membership.ManagementGroup != managementGroup {
				continue
			}

			for _, role := range membership.Roles {
				out = append(out, expectedRoleState{
					NodeID: nodeID,
					Role:   role,
				})
			}
		}
	}

	return out
}

// applyPriorityPolicy enforces priority-based desired states for all management groups in a cluster group.
// In automatic mode it also handles priority swaps on failure. Always uses two-phase switchover to avoid
// simultaneous active groups during transitions.
func (c *Controller) applyPriorityPolicy(
	ctx context.Context,
	clusterGroup string,
	groups []groupRuntime,
) error {
	if len(groups) == 0 {
		return nil
	}

	// Automatic mode: when the top-priority group has failed, swap priorities with the next best.
	if c.cfg.Cluster.FailoverMode == "automatic" {
		if c.handleAutoFailover(ctx, clusterGroup, groups) {
			return nil // priorities written; next reconcile applies them
		}
	}

	// Active-active topology: all groups share the same priority → activate all non-idle groups.
	if allSamePriority(groups) {
		for _, g := range groups {
			if g.Desired == model.DesiredIdle {
				continue
			}
			if err := c.writeDesiredIfChanged(ctx, g.ClusterGroup, g.ManagementGroup, model.DesiredActive); err != nil {
				return err
			}
		}
		return nil
	}

	// Active-passive topology: find the highest-priority available group.
	target := c.findTarget(clusterGroup, groups)
	if target == nil {
		c.log.Debug("no available management groups for failover policy", zap.String("cluster_group", clusterGroup))
		return nil
	}

	// Determine which group currently holds active ownership.
	// Primary signal: desired=active. Secondary: desired=passive but actual is still
	// active/stopping — the group is mid-draining (Phase 1 already wrote desired=passive).
	currentActive := ""
	for _, g := range groups {
		if g.Desired == model.DesiredActive {
			currentActive = g.ManagementGroup
			break
		}
	}
	if currentActive == "" {
		for _, g := range groups {
			if g.Desired == model.DesiredPassive &&
				(g.Actual == model.ActualActive || g.Actual == model.ActualStopping || g.Actual == model.ActualStarting) {
				currentActive = g.ManagementGroup
				break
			}
		}
	}

	// Already correct: ensure all other non-idle groups are passive.
	if currentActive == target.ManagementGroup {
		for _, g := range groups {
			if g.ManagementGroup == target.ManagementGroup {
				continue
			}
			if g.Desired == model.DesiredIdle {
				continue
			}
			if err := c.writeDesiredIfChanged(ctx, g.ClusterGroup, g.ManagementGroup, model.DesiredPassive); err != nil {
				return err
			}
		}
		return nil
	}

	// Two-phase switchover needed.
	// Phase 1: wait for current active group to reach passive or failed.
	if currentActive != "" {
		var curActual model.ActualState
		for _, g := range groups {
			if g.ManagementGroup == currentActive {
				curActual = g.Actual
				break
			}
		}

		if curActual != model.ActualPassive && curActual != model.ActualFailed && curActual != model.ActualIdle {
			if err := c.writeDesiredIfChanged(ctx, clusterGroup, currentActive, model.DesiredPassive); err != nil {
				return err
			}

			switchKey := clusterGroup + "/" + currentActive
			if _, ok := c.switchoverStarted[switchKey]; !ok {
				c.switchoverStarted[switchKey] = time.Now()
				// Record which target this phase-1 drain is for, so phase-2 can verify
				// it was reached via a legitimate controller-initiated switchover and not
				// a stray desired=passive left over from a disable or other admin action.
				c.switchoverTarget[clusterGroup] = target.ManagementGroup
			} else {
				T := c.switchoverTimeout(clusterGroup, currentActive)
				if time.Since(c.switchoverStarted[switchKey]) > T {
					c.log.Warn("switchover timeout: group did not reach passive/failed in time",
						zap.String("cluster_group", clusterGroup),
						zap.String("management_group", currentActive),
						zap.Duration("timeout", T),
					)
					delete(c.switchoverStarted, switchKey)
					delete(c.switchoverTarget, clusterGroup)
				}
			}
			return nil
		}

		delete(c.switchoverStarted, clusterGroup+"/"+currentActive)
	}

	// Phase 2: activate target group.
	// Guard: only proceed if phase-1 was controller-initiated for this target. This prevents
	// the controller from auto-activating a passive group when the previous active was
	// intentionally disabled (desired=idle) by an admin — in that case phase-1 never fires
	// and switchoverTarget is never set.
	if currentActive == "" && c.switchoverTarget[clusterGroup] != target.ManagementGroup {
		return nil
	}
	delete(c.switchoverTarget, clusterGroup)

	if err := c.writeDesiredIfChanged(ctx, target.ClusterGroup, target.ManagementGroup, model.DesiredActive); err != nil {
		return err
	}

	for _, g := range groups {
		if g.ManagementGroup == target.ManagementGroup {
			continue
		}
		if g.Desired == model.DesiredIdle {
			continue
		}
		if err := c.writeDesiredIfChanged(ctx, g.ClusterGroup, g.ManagementGroup, model.DesiredPassive); err != nil {
			return err
		}
	}

	return nil
}

// allSamePriority returns true if all groups in the slice share the same priority value.
func allSamePriority(groups []groupRuntime) bool {
	if len(groups) == 0 {
		return true
	}
	p := groups[0].Priority
	for _, g := range groups[1:] {
		if g.Priority != p {
			return false
		}
	}
	return true
}

// findTarget returns the highest-priority (lowest Priority value) available group.
// Groups with desired=idle are always excluded from candidacy.
func (c *Controller) findTarget(clusterGroup string, groups []groupRuntime) *groupRuntime {
	var result *groupRuntime
	for i := range groups {
		g := &groups[i]
		if g.Desired == model.DesiredIdle {
			continue
		}
		if !g.Available {
			continue
		}
		if result == nil || g.Priority < result.Priority {
			result = g
		}
	}
	return result
}

// switchoverTimeout calculates the worst-case time for a management group to reach passive/failed,
// derived from its roles' check_interval + converge + exec timeouts. Falls back to 30s if unknown.
func (c *Controller) switchoverTimeout(clusterGroup, managementGroup string) time.Duration {
	mgMap, ok := c.cfg.ManagementGroups[clusterGroup]
	if !ok {
		return 30 * time.Second
	}
	mgCfg, ok := mgMap[managementGroup]
	if !ok {
		return 30 * time.Second
	}

	var maxT time.Duration
	for _, roleID := range mgCfg.Roles {
		roleCfg, ok := c.cfg.Roles[roleID]
		if !ok {
			continue
		}
		t := roleCfg.Timeouts.CheckInterval.Duration +
			roleCfg.Timeouts.Converge.Duration +
			roleCfg.Timeouts.Exec.Duration
		if t > maxT {
			maxT = t
		}
	}

	if maxT == 0 {
		return 30 * time.Second
	}
	return maxT
}

// writePriority updates the Priority field of a management group's config document in etcd.
func (c *Controller) writePriority(ctx context.Context, clusterGroup, managementGroup string, priority int) {
	key := model.ManagementGroupConfig(c.cfg.Cluster.ID, clusterGroup, managementGroup)

	raw, ok := c.store.Get(key)
	if !ok {
		c.log.Warn("management group config not found for priority update",
			zap.String("cluster_group", clusterGroup),
			zap.String("management_group", managementGroup),
		)
		return
	}

	var doc model.ManagementGroupConfigDocument
	if err := json.Unmarshal(raw, &doc); err != nil {
		c.log.Error("failed to decode management group config for priority update", zap.Error(err))
		return
	}

	if doc.Priority == priority {
		return
	}

	doc.Priority = priority
	doc.UpdatedAt = time.Now().UTC()

	data, err := json.Marshal(doc)
	if err != nil {
		c.log.Error("failed to marshal management group config", zap.Error(err))
		return
	}

	c.log.Info("writing management group priority",
		zap.String("cluster_group", clusterGroup),
		zap.String("management_group", managementGroup),
		zap.Int("priority", priority),
	)

	if err := c.etcd.Put(ctx, key, data); err != nil {
		c.log.Error("failed to write management group priority", zap.Error(err))
	}
}

// handleAutoFailover detects when the top-priority group has failed and swaps its priority with
// the next best available group. Returns true if priorities were changed (caller should skip
// further policy application and wait for the next reconcile cycle).
func (c *Controller) handleAutoFailover(ctx context.Context, clusterGroup string, groups []groupRuntime) bool {
	// Find the minimum priority value.
	minPri := -1
	for _, g := range groups {
		if minPri < 0 || g.Priority < minPri {
			minPri = g.Priority
		}
	}
	if minPri < 0 {
		return false
	}

	// Check if any top-priority group is unavailable (failed) due to an unintended failure.
	// Groups with desired=idle are excluded — intentional admin actions do not trigger auto-failover.
	topFailed := false
	for _, g := range groups {
		if g.Priority == minPri && !g.Available && g.Desired != model.DesiredIdle {
			topFailed = true
			break
		}
	}
	if !topFailed {
		return false
	}

	// Find the next best priority among available groups.
	nextPri := -1
	for _, g := range groups {
		if !g.Available {
			continue
		}
		if nextPri < 0 || g.Priority < nextPri {
			nextPri = g.Priority
		}
	}
	if nextPri < 0 {
		c.log.Warn("auto failover: no available groups to promote", zap.String("cluster_group", clusterGroup))
		return false
	}

	// Swap: failed top-priority groups get nextPri; next-best available groups get minPri.
	for _, g := range groups {
		if g.Priority == minPri && !g.Available {
			c.writePriority(ctx, clusterGroup, g.ManagementGroup, nextPri)
			c.log.Info("auto failover: demoted failed group",
				zap.String("cluster_group", clusterGroup),
				zap.String("management_group", g.ManagementGroup),
				zap.Int("old_priority", minPri),
				zap.Int("new_priority", nextPri),
			)
		} else if g.Priority == nextPri && g.Available {
			c.writePriority(ctx, clusterGroup, g.ManagementGroup, minPri)
			c.log.Info("auto failover: promoted group",
				zap.String("cluster_group", clusterGroup),
				zap.String("management_group", g.ManagementGroup),
				zap.Int("old_priority", nextPri),
				zap.Int("new_priority", minPri),
			)
		}
	}

	return true
}

func (c *Controller) writeDesiredIfChanged(
	ctx context.Context,
	clusterGroup string,
	managementGroup string,
	target model.DesiredState,
) error {
	key := model.Desired(c.cfg.Cluster.ID, clusterGroup, managementGroup)

	raw, exists := c.store.Get(key)
	if exists {
		var current model.DesiredDocument
		if err := json.Unmarshal(raw, &current); err == nil {
			if current.State == target {
				return nil
			}
		}
	}

	doc := model.DesiredDocument{
		State:     target,
		UpdatedAt: time.Now().UTC(),
	}

	data, err := json.Marshal(doc)
	if err != nil {
		return err
	}

	c.log.Debug(
		"writing desired from priority policy",
		zap.String("cluster_group", clusterGroup),
		zap.String("management_group", managementGroup),
		zap.String("desired", string(target)),
	)

	return c.etcd.Put(ctx, key, data)
}

func (c *Controller) readPriority(clusterGroup string, managementGroup string) int {
	key := model.ManagementGroupConfig(c.cfg.Cluster.ID, clusterGroup, managementGroup)

	raw, ok := c.store.Get(key)
	if !ok {
		return 1000
	}

	var doc model.ManagementGroupConfigDocument
	if err := json.Unmarshal(raw, &doc); err != nil {
		return 1000
	}

	if doc.Priority <= 0 {
		return 1000
	}

	return doc.Priority
}

// buildActualIfChanged serialises a new ActualDocument only when state or details differ from the stored value.
func (c *Controller) buildActualIfChanged(
	key string,
	state model.ActualState,
	details string,
	updatedAt time.Time,
) (bool, []byte, error) {
	next := model.ActualDocument{
		State:     state,
		UpdatedAt: updatedAt,
		Details:   details,
	}

	currentRaw, exists := c.store.Get(key)
	if exists {
		var current model.ActualDocument
		if err := json.Unmarshal(currentRaw, &current); err == nil {
			if current.State == next.State && current.Details == next.Details {
				return false, nil, nil
			}
		}
	}

	data, err := json.Marshal(next)
	if err != nil {
		return false, nil, err
	}

	if exists && bytes.Equal(bytes.TrimSpace(currentRaw), data) {
		return false, nil, nil
	}

	return true, data, nil
}

// buildHealthIfChanged serialises a new HealthDocument only when status or details differ from the stored value.
func (c *Controller) buildHealthIfChanged(
	key string,
	status model.HealthStatus,
	details string,
	updatedAt time.Time,
) (bool, []byte, error) {
	next := model.HealthDocument{
		Status:    status,
		UpdatedAt: updatedAt,
		Details:   details,
	}

	currentRaw, exists := c.store.Get(key)
	if exists {
		var current model.HealthDocument
		if err := json.Unmarshal(currentRaw, &current); err == nil {
			if current.Status == next.Status && current.Details == next.Details {
				return false, nil, nil
			}
		}
	}

	data, err := json.Marshal(next)
	if err != nil {
		return false, nil, err
	}

	if exists && bytes.Equal(bytes.TrimSpace(currentRaw), data) {
		return false, nil, nil
	}

	return true, data, nil
}

func isSystemKey(name string) bool {
	switch name {
	case "leadership", "commands", "commands_history", "registry", "session", "config":
		return true
	default:
		return false
	}
}

func formatMissingRoleStates(missing []string) string {
	if len(missing) == 0 {
		return ""
	}

	return fmt.Sprintf("agent lost: %s", strings.Join(missing, ","))
}
