package controller

import (
	"bytes"
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"time"

	"cluster-agent/internal/config"
	"cluster-agent/internal/etcd"
	"cluster-agent/internal/keys"
	"cluster-agent/internal/model"
	"cluster-agent/internal/store"
	"go.uber.org/zap"
)

type Controller struct {
	cfg   *config.Config
	store *store.StateStore
	etcd  *etcd.Client
	log   *zap.Logger

	lastManagementGroups map[string][]string
}

type groupRuntime struct {
	ClusterGroup    string
	ManagementGroup string
	Priority        int
	Actual          model.ActualState
	Health          model.HealthStatus
	Available       bool
}

func New(cfg *config.Config, st *store.StateStore, etcdClient *etcd.Client, log *zap.Logger) *Controller {
	return &Controller{
		cfg:                  cfg,
		store:                st,
		etcd:                 etcdClient,
		log:                  log,
		lastManagementGroups: make(map[string][]string),
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

func (c *Controller) Reconcile(ctx context.Context) error {
	if !c.store.Ready() {
		c.log.Debug("state store is not ready, skip reconcile")
		return nil
	}

	for _, clusterGroup := range c.cfg.Cluster.Groups {
		groupPrefix := keys.ClusterGroup(c.cfg.Cluster.ID, clusterGroup)
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

		if c.cfg.Cluster.FailoverMode == "automatic" {
			if err := c.applyPriorityPolicy(ctx, clusterGroup, runtimes); err != nil {
				c.log.Error(
					"priority policy failed",
					zap.String("cluster_group", clusterGroup),
					zap.Error(err),
				)
			}
		}
	}

	return nil
}

func (c *Controller) reconcileManagementGroup(
	ctx context.Context,
	clusterGroup string,
	managementGroup string,
) (groupRuntime, error) {

	prefix := keys.ManagementGroup(c.cfg.Cluster.ID, clusterGroup, managementGroup)
	items := c.store.Prefix(prefix)

	now := time.Now().UTC()

	// ---- новая агрегация ----
	var (
		seenRoles    bool
		seenActive   bool
		seenPassive  bool
		seenUnstable bool
	)

	actualState := model.ActualIdle
	healthStatus := model.HealthWarning
	details := "empty management group"

	for key, value := range items {
		if !strings.HasSuffix(key, "/actual") {
			continue
		}

		if key == keys.Actual(c.cfg.Cluster.ID, clusterGroup, managementGroup) {
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

		case model.ActualStarting, model.ActualStopping, model.ActualIdle:
			seenUnstable = true
		}

		if actualState == model.ActualFailed {
			break
		}
	}

	if actualState != model.ActualFailed {
		switch {
		case !seenRoles:
			actualState = model.ActualIdle
			healthStatus = model.HealthWarning
			details = "empty management group"

		case seenUnstable:
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

	// ---- запись aggregate ----

	actualKey := keys.Actual(c.cfg.Cluster.ID, clusterGroup, managementGroup)
	healthKey := keys.Health(c.cfg.Cluster.ID, clusterGroup, managementGroup)

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
		Actual:          actualState,
		Health:          healthStatus,
		Available:       healthStatus != model.HealthFailed && actualState != model.ActualFailed,
	}, nil
}

func (c *Controller) applyPriorityPolicy(
	ctx context.Context,
	clusterGroup string,
	groups []groupRuntime,
) error {

	bestPriority := 0
	hasCandidate := false

	for _, group := range groups {
		if !group.Available {
			continue
		}

		if !hasCandidate || group.Priority < bestPriority {
			bestPriority = group.Priority
			hasCandidate = true
		}
	}

	if !hasCandidate {
		c.log.Warn("no available management groups for automatic failover", zap.String("cluster_group", clusterGroup))
		return nil
	}

	for _, group := range groups {
		target := model.DesiredPassive

		if group.Available && group.Priority == bestPriority {
			target = model.DesiredActive
		}

		if err := c.writeDesiredIfChanged(ctx, group.ClusterGroup, group.ManagementGroup, target); err != nil {
			return err
		}
	}

	return nil
}

func (c *Controller) writeDesiredIfChanged(
	ctx context.Context,
	clusterGroup string,
	managementGroup string,
	target model.DesiredState,
) error {

	key := keys.Desired(c.cfg.Cluster.ID, clusterGroup, managementGroup)

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
	key := keys.ManagementGroupConfig(c.cfg.Cluster.ID, clusterGroup, managementGroup)

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
