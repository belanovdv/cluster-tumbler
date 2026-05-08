// commands.go implements the leader-side command consumer that reads from commands/ and executes them.
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
	case model.CommandTypeDisable:
		execErr = cc.execDisable(ctx, cmd)
	case model.CommandTypeReload:
		execErr = cc.execReload(ctx, cmd)
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
		return nil
	}

	// Swap: target gets minPri, current top gets targetPri.
	if err := cc.writePriority(ctx, cmd.ClusterGroup, cmd.ManagementGroup, minPri); err != nil {
		return fmt.Errorf("writing priority for %s: %w", cmd.ManagementGroup, err)
	}
	if err := cc.writePriority(ctx, cmd.ClusterGroup, minGroup, targetPri); err != nil {
		return fmt.Errorf("writing priority for %s: %w", minGroup, err)
	}

	cc.log.Info("promote: priorities swapped",
		zap.String("promoted", cmd.ManagementGroup), zap.Int("new_priority", minPri),
		zap.String("demoted", minGroup), zap.Int("new_priority", targetPri),
	)
	return nil
}

// execDisable sets desired=idle for the management group, taking it out of active management.
func (cc *CommandConsumer) execDisable(ctx context.Context, cmd model.Command) error {
	return cc.writeDesired(ctx, cmd.ClusterGroup, cmd.ManagementGroup, model.DesiredIdle)
}

// execReload clears the failed state by writing desired=passive, triggering a fresh passive convergence attempt.
func (cc *CommandConsumer) execReload(ctx context.Context, cmd model.Command) error {
	return cc.writeDesired(ctx, cmd.ClusterGroup, cmd.ManagementGroup, model.DesiredPassive)
}

func (cc *CommandConsumer) writeDesired(ctx context.Context, clusterGroup, managementGroup string, state model.DesiredState) error {
	doc := model.DesiredDocument{
		State:     state,
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
	}
}
