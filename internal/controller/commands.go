// commands.go implements the leader-side command consumer: reads commands/ queue and executes promote, demote, disable, enable, and force_passive.
package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"cluster-tumbler/internal/config"
	"cluster-tumbler/internal/etcd"
	"cluster-tumbler/internal/model"
	"cluster-tumbler/internal/store"

	"go.uber.org/zap"
)

// CommandConsumer reads pending commands from the etcd commands/ queue and executes them.
// It runs only on the leader node.
type CommandConsumer struct {
	clusterID string
	cfg       *config.Config
	store     *store.StateStore
	etcd      *etcd.Client
	log       *zap.Logger
}

func NewCommandConsumer(cfg *config.Config, st *store.StateStore, etcdClient *etcd.Client, log *zap.Logger) *CommandConsumer {
	return &CommandConsumer{
		clusterID: cfg.Cluster.ID,
		cfg:       cfg,
		store:     st,
		etcd:      etcdClient,
		log:       log,
	}
}

// Run drains any pending commands present on startup then watches for new ones.
func (cc *CommandConsumer) Run(ctx context.Context) error {
	cc.log.Debug("starting command consumer")

	cc.drainPending(ctx)

	watchCh := cc.etcd.WatchPrefix(ctx, model.CommandsKey(cc.clusterID), cc.store.Revision()+1)

	for {
		select {
		case event, ok := <-watchCh:
			if !ok {
				return nil
			}
			if event.Type != store.EventPut {
				continue
			}
			var cmd model.Command
			if err := json.Unmarshal(event.Value, &cmd); err != nil {
				cc.log.Error("failed to decode command event", zap.Error(err))
				continue
			}
			if cmd.Status == model.CommandPending {
				cc.processCommand(ctx, cmd)
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// drainPending processes all pending commands already present in the store at startup.
func (cc *CommandConsumer) drainPending(ctx context.Context) {
	items := cc.store.Prefix(model.CommandsKey(cc.clusterID))
	for _, raw := range items {
		var cmd model.Command
		if err := json.Unmarshal(raw, &cmd); err != nil {
			continue
		}
		if cmd.Status == model.CommandPending {
			cc.processCommand(ctx, cmd)
		}
	}
}

// processCommand marks a command as running, executes it, updates its status, and archives it.
func (cc *CommandConsumer) processCommand(ctx context.Context, cmd model.Command) {
	cc.log.Info("processing command",
		zap.String("id", cmd.ID),
		zap.String("type", string(cmd.Type)),
		zap.String("cluster_group", cmd.ClusterGroup),
		zap.String("management_group", cmd.ManagementGroup),
	)

	now := time.Now().UTC()
	cmd.Status = model.CommandRunning
	cmd.StartedAt = &now
	cc.writeCommand(ctx, cmd)

	var execErr error
	switch cmd.Type {
	case model.CommandTypePromote:
		execErr = cc.execPromote(ctx, cmd)
	case model.CommandTypeDemote:
		execErr = cc.execDemote(ctx, cmd)
	case model.CommandTypeDisable:
		execErr = cc.execDisable(ctx, cmd)
	case model.CommandTypeEnable:
		execErr = cc.execEnable(ctx, cmd)
	case model.CommandTypeForcePassive:
		execErr = cc.execForcePassive(ctx, cmd)
	default:
		execErr = fmt.Errorf("unknown command type: %s", cmd.Type)
	}

	finished := time.Now().UTC()
	cmd.FinishedAt = &finished

	if execErr != nil {
		cc.log.Error("command failed", zap.String("id", cmd.ID), zap.Error(execErr))
		cmd.Status = model.CommandFailed
		cmd.Error = execErr.Error()
	} else {
		cc.log.Info("command completed", zap.String("id", cmd.ID))
		cmd.Status = model.CommandCompleted
	}

	cc.writeCommand(ctx, cmd)
	cc.archiveCommand(ctx, cmd)
}

// execPromote swaps priorities so the target management group becomes the highest-priority group.
// The controller picks up the updated priorities on the next reconcile cycle and applies
// the two-phase switchover automatically.
func (cc *CommandConsumer) execPromote(ctx context.Context, cmd model.Command) error {
	children := cc.store.ListChildren(model.ClusterGroup(cc.clusterID, cmd.ClusterGroup))

	type entry struct {
		mg       string
		priority int
	}
	var entries []entry
	for _, mg := range children {
		raw, ok := cc.store.Get(model.ManagementGroupConfig(cc.clusterID, cmd.ClusterGroup, mg))
		if !ok {
			continue
		}
		var doc model.ManagementGroupConfigDocument
		if err := json.Unmarshal(raw, &doc); err != nil {
			continue
		}
		entries = append(entries, entry{mg: mg, priority: doc.Priority})
	}

	// Find target's current priority.
	targetPri := -1
	for _, e := range entries {
		if e.mg == cmd.ManagementGroup {
			targetPri = e.priority
			break
		}
	}
	if targetPri < 0 {
		return fmt.Errorf("management group %q not found in cluster group %q", cmd.ManagementGroup, cmd.ClusterGroup)
	}

	// Find current top-priority group (minimum priority value).
	minPri := targetPri
	minGroup := cmd.ManagementGroup
	for _, e := range entries {
		if e.priority < minPri {
			minPri = e.priority
			minGroup = e.mg
		}
	}

	if minGroup == cmd.ManagementGroup {
		cc.log.Debug("promote: group already has highest priority",
			zap.String("management_group", cmd.ManagementGroup),
		)
		return cc.activateTarget(ctx, cmd.ClusterGroup, cmd.ManagementGroup)
	}

	// Swap: target gets minPri, current top gets targetPri.
	if err := cc.writePriority(ctx, cmd.ClusterGroup, cmd.ManagementGroup, minPri); err != nil {
		return fmt.Errorf("writing priority for %s: %w", cmd.ManagementGroup, err)
	}
	if err := cc.writePriority(ctx, cmd.ClusterGroup, minGroup, targetPri); err != nil {
		return fmt.Errorf("writing priority for %s: %w", minGroup, err)
	}

	if err := cc.activateTarget(ctx, cmd.ClusterGroup, cmd.ManagementGroup); err != nil {
		return fmt.Errorf("activating target %s: %w", cmd.ManagementGroup, err)
	}

	cc.log.Info("promote: priorities swapped",
		zap.String("promoted", cmd.ManagementGroup), zap.Int("new_priority", minPri),
		zap.String("demoted", minGroup), zap.Int("new_priority", targetPri),
	)
	return nil
}

// execDisable sets managed=false on the management group, preserving its current desired state.
// The controller stops managing the group; role workers switch to probe-only mode.
func (cc *CommandConsumer) execDisable(ctx context.Context, cmd model.Command) error {
	currentState := model.DesiredPassive
	if raw, ok := cc.store.Get(model.Desired(cc.clusterID, cmd.ClusterGroup, cmd.ManagementGroup)); ok {
		var doc model.DesiredDocument
		if err := json.Unmarshal(raw, &doc); err == nil {
			currentState = doc.State
		}
	}
	return cc.writeDesired(ctx, cmd.ClusterGroup, cmd.ManagementGroup, currentState, false)
}

// execEnable sets managed=true, restoring the group to normal management while preserving the current desired state.
func (cc *CommandConsumer) execEnable(ctx context.Context, cmd model.Command) error {
	currentState := model.DesiredPassive
	if raw, ok := cc.store.Get(model.Desired(cc.clusterID, cmd.ClusterGroup, cmd.ManagementGroup)); ok {
		var doc model.DesiredDocument
		if err := json.Unmarshal(raw, &doc); err == nil {
			currentState = doc.State
		}
	}
	return cc.writeDesired(ctx, cmd.ClusterGroup, cmd.ManagementGroup, currentState, true)
}

// execDemote handles two cases:
//   - desired=passive: re-triggers passive convergence (equivalent to former reload).
//   - desired=active: auto-selects the best passive replacement (managed=true, actual=passive,
//     health=ok, highest priority), swaps priorities, writes desired=passive to the demoted
//     group and desired=active to the replacement. The controller's two-phase switchover then
//     waits for the demoted group to reach actual=passive before activating the replacement.
func (cc *CommandConsumer) execDemote(ctx context.Context, cmd model.Command) error {
	raw, ok := cc.store.Get(model.Desired(cc.clusterID, cmd.ClusterGroup, cmd.ManagementGroup))
	if !ok {
		return fmt.Errorf("desired state not found for %s/%s", cmd.ClusterGroup, cmd.ManagementGroup)
	}
	var desiredDoc model.DesiredDocument
	if err := json.Unmarshal(raw, &desiredDoc); err != nil {
		return fmt.Errorf("failed to decode desired state: %w", err)
	}

	if desiredDoc.State == model.DesiredPassive {
		return cc.writeDesired(ctx, cmd.ClusterGroup, cmd.ManagementGroup, model.DesiredPassive, true)
	}

	// Find best replacement: managed=true, actual=passive, health=ok, lowest priority number.
	type candidate struct {
		mg       string
		priority int
	}
	var best *candidate

	for _, mg := range cc.store.ListChildren(model.ClusterGroup(cc.clusterID, cmd.ClusterGroup)) {
		if mg == cmd.ManagementGroup {
			continue
		}
		desRaw, ok := cc.store.Get(model.Desired(cc.clusterID, cmd.ClusterGroup, mg))
		if !ok {
			continue
		}
		var des model.DesiredDocument
		if err := json.Unmarshal(desRaw, &des); err != nil || !des.Managed {
			continue
		}
		actRaw, ok := cc.store.Get(model.Actual(cc.clusterID, cmd.ClusterGroup, mg))
		if !ok {
			continue
		}
		var act model.ActualDocument
		if err := json.Unmarshal(actRaw, &act); err != nil || act.State != model.ActualPassive {
			continue
		}
		healthRaw, ok := cc.store.Get(model.Health(cc.clusterID, cmd.ClusterGroup, mg))
		if !ok {
			continue
		}
		var h model.HealthDocument
		if err := json.Unmarshal(healthRaw, &h); err != nil || h.Status != model.HealthOK {
			continue
		}
		cfgRaw, ok := cc.store.Get(model.ManagementGroupConfig(cc.clusterID, cmd.ClusterGroup, mg))
		if !ok {
			continue
		}
		var cfg model.ManagementGroupConfigDocument
		if err := json.Unmarshal(cfgRaw, &cfg); err != nil {
			continue
		}
		if best == nil || cfg.Priority < best.priority {
			best = &candidate{mg: mg, priority: cfg.Priority}
		}
	}

	if best == nil {
		return fmt.Errorf("no available passive managed group with health=ok in cluster group %q", cmd.ClusterGroup)
	}

	demotedCfgRaw, ok := cc.store.Get(model.ManagementGroupConfig(cc.clusterID, cmd.ClusterGroup, cmd.ManagementGroup))
	if !ok {
		return fmt.Errorf("config not found for %s", cmd.ManagementGroup)
	}
	var demotedCfg model.ManagementGroupConfigDocument
	if err := json.Unmarshal(demotedCfgRaw, &demotedCfg); err != nil {
		return fmt.Errorf("failed to decode config for %s: %w", cmd.ManagementGroup, err)
	}

	if err := cc.writePriority(ctx, cmd.ClusterGroup, best.mg, demotedCfg.Priority); err != nil {
		return fmt.Errorf("writing priority for %s: %w", best.mg, err)
	}
	if err := cc.writePriority(ctx, cmd.ClusterGroup, cmd.ManagementGroup, best.priority); err != nil {
		return fmt.Errorf("writing priority for %s: %w", cmd.ManagementGroup, err)
	}
	if err := cc.writeDesired(ctx, cmd.ClusterGroup, cmd.ManagementGroup, model.DesiredPassive, true); err != nil {
		return fmt.Errorf("writing desired for %s: %w", cmd.ManagementGroup, err)
	}
	if err := cc.writeDesired(ctx, cmd.ClusterGroup, best.mg, model.DesiredActive, true); err != nil {
		return fmt.Errorf("writing desired for %s: %w", best.mg, err)
	}

	cc.log.Info("demote: switchover initiated",
		zap.String("demoted", cmd.ManagementGroup), zap.Int("new_priority", best.priority),
		zap.String("replacement", best.mg), zap.Int("new_priority", demotedCfg.Priority),
	)
	return nil
}

// execForcePassive transfers a managed=false group with desired=active to desired=passive,
// setting managed=true so the role workers run set_passive and bring services down.
// The controller does not auto-activate any other group because switchoverTarget is not set.
func (cc *CommandConsumer) execForcePassive(ctx context.Context, cmd model.Command) error {
	raw, ok := cc.store.Get(model.Desired(cc.clusterID, cmd.ClusterGroup, cmd.ManagementGroup))
	if !ok {
		return fmt.Errorf("desired state not found for %s/%s", cmd.ClusterGroup, cmd.ManagementGroup)
	}
	var doc model.DesiredDocument
	if err := json.Unmarshal(raw, &doc); err != nil {
		return fmt.Errorf("failed to decode desired state: %w", err)
	}
	if doc.Managed {
		return fmt.Errorf("force_passive requires managed=false, group is under normal management")
	}
	return cc.writeDesired(ctx, cmd.ClusterGroup, cmd.ManagementGroup, model.DesiredPassive, true)
}

// activateTarget writes desired=active when no managed group is currently active (bootstrap),
// otherwise writes desired=passive, managed=true so the controller runs two-phase switchover.
func (cc *CommandConsumer) activateTarget(ctx context.Context, clusterGroup, managementGroup string) error {
	if cc.hasNoActiveGroup(clusterGroup) {
		return cc.writeDesired(ctx, clusterGroup, managementGroup, model.DesiredActive, true)
	}
	return cc.writeDesired(ctx, clusterGroup, managementGroup, model.DesiredPassive, true)
}

// hasNoActiveGroup returns true when no managed group (managed=true) has desired=active.
func (cc *CommandConsumer) hasNoActiveGroup(clusterGroup string) bool {
	for _, mg := range cc.store.ListChildren(model.ClusterGroup(cc.clusterID, clusterGroup)) {
		raw, ok := cc.store.Get(model.Desired(cc.clusterID, clusterGroup, mg))
		if !ok {
			continue
		}
		var doc model.DesiredDocument
		if err := json.Unmarshal(raw, &doc); err != nil {
			continue
		}
		if doc.State == model.DesiredActive && doc.Managed {
			return false
		}
	}
	return true
}

func (cc *CommandConsumer) writeDesired(ctx context.Context, clusterGroup, managementGroup string, state model.DesiredState, managed bool) error {
	doc := model.DesiredDocument{
		State:     state,
		Managed:   managed,
		UpdatedAt: time.Now().UTC(),
	}
	data, err := json.Marshal(doc)
	if err != nil {
		return err
	}
	return cc.etcd.Put(ctx, model.Desired(cc.clusterID, clusterGroup, managementGroup), data)
}

func (cc *CommandConsumer) writePriority(ctx context.Context, clusterGroup, managementGroup string, priority int) error {
	key := model.ManagementGroupConfig(cc.clusterID, clusterGroup, managementGroup)
	raw, ok := cc.store.Get(key)
	if !ok {
		return fmt.Errorf("management group config not found: %s/%s", clusterGroup, managementGroup)
	}

	var doc model.ManagementGroupConfigDocument
	if err := json.Unmarshal(raw, &doc); err != nil {
		return err
	}

	doc.Priority = priority
	doc.UpdatedAt = time.Now().UTC()

	data, err := json.Marshal(doc)
	if err != nil {
		return err
	}
	return cc.etcd.Put(ctx, key, data)
}

func (cc *CommandConsumer) writeCommand(ctx context.Context, cmd model.Command) {
	data, err := json.Marshal(cmd)
	if err != nil {
		cc.log.Error("failed to marshal command", zap.Error(err))
		return
	}
	if err := cc.etcd.Put(ctx, model.CommandKey(cc.clusterID, cmd.ID), data); err != nil {
		cc.log.Error("failed to write command status", zap.Error(err))
	}
}

func (cc *CommandConsumer) archiveCommand(ctx context.Context, cmd model.Command) {
	data, err := json.Marshal(cmd)
	if err != nil {
		cc.log.Error("failed to marshal command for archive", zap.Error(err))
		return
	}
	if err := cc.etcd.Put(ctx, model.CommandHistoryKey(cc.clusterID, cmd.ID), data); err != nil {
		cc.log.Error("failed to archive command", zap.Error(err))
		return
	}
	if err := cc.etcd.Delete(ctx, model.CommandKey(cc.clusterID, cmd.ID)); err != nil {
		cc.log.Error("failed to delete command from queue", zap.String("id", cmd.ID), zap.Error(err))
	}
}
