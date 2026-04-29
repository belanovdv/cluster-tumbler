package api

import (
	"encoding/json"
	"sort"

	"cluster-tumbler/internal/model"
	"cluster-tumbler/internal/store"
)

type StateView struct {
	Ready    bool         `json:"ready"`
	Revision int64        `json:"revision"`
	Cluster  *ClusterView `json:"cluster"`
}

type ClusterView struct {
	ID         string                                `json:"id"`
	Name       string                                `json:"name"`
	Leadership json.RawMessage                       `json:"leadership,omitempty"`
	Config     map[string]map[string]json.RawMessage `json:"config,omitempty"`
	Registry   map[string]json.RawMessage            `json:"registry,omitempty"`
	Session    map[string]json.RawMessage            `json:"session,omitempty"`
	Groups     map[string]*ClusterGroupView          `json:"groups"`
}

type ClusterGroupView struct {
	ID               string                          `json:"id"`
	Name             string                          `json:"name"`
	ManagementGroups map[string]*ManagementGroupView `json:"management_groups"`
}

type ManagementGroupView struct {
	ID      string               `json:"id"`
	Name    string               `json:"name"`
	Config  json.RawMessage      `json:"config,omitempty"`
	Desired json.RawMessage      `json:"desired,omitempty"`
	Actual  json.RawMessage      `json:"actual,omitempty"`
	Health  json.RawMessage      `json:"health,omitempty"`
	Nodes   map[string]*NodeView `json:"nodes,omitempty"`
}

type NodeView struct {
	ID    string               `json:"id"`
	Name  string               `json:"name"`
	Roles map[string]*RoleView `json:"roles,omitempty"`
}

type RoleView struct {
	ID     string          `json:"id"`
	Name   string          `json:"name"`
	Actual json.RawMessage `json:"actual,omitempty"`
	Health json.RawMessage `json:"health,omitempty"`
}

func BuildStateView(clusterID string, ready bool, revision int64, root *store.TreeNode) StateView {
	view := StateView{
		Ready:    ready,
		Revision: revision,
		Cluster: &ClusterView{
			ID:     clusterID,
			Name:   clusterID,
			Groups: make(map[string]*ClusterGroupView),
		},
	}

	clusterRoot := child(root, clusterID)
	clusterNode := child(clusterRoot, "cluster")
	if clusterNode == nil {
		return view
	}

	meta := buildDynamicConfigMeta(child(child(clusterNode, "config"), "_meta"))

	view.Cluster.Name = nameOrID(meta.Cluster.Name, clusterID)

	if leadership := child(clusterNode, "leadership"); leadership != nil {
		view.Cluster.Leadership = valueOf(leadership)
	}

	view.Cluster.Config = buildConfig(child(clusterNode, "config"))
	view.Cluster.Registry = buildFlatValueMap(child(clusterNode, "registry"))
	view.Cluster.Session = buildFlatValueMap(child(clusterNode, "session"))

	for _, clusterGroupID := range sortedChildNames(clusterNode) {
		if isRootSystemKey(clusterGroupID) {
			continue
		}

		clusterGroupNode := child(clusterNode, clusterGroupID)
		if clusterGroupNode == nil {
			continue
		}

		view.Cluster.Groups[clusterGroupID] = buildClusterGroup(
			clusterGroupID,
			clusterGroupNode,
			view.Cluster.Config[clusterGroupID],
			meta,
		)
	}

	return view
}

func buildDynamicConfigMeta(node *store.TreeNode) model.DynamicConfigDocument {
	raw := valueOf(node)
	if raw == nil {
		return model.DynamicConfigDocument{}
	}

	var meta model.DynamicConfigDocument
	if err := json.Unmarshal(raw, &meta); err != nil {
		return model.DynamicConfigDocument{}
	}

	return meta
}

func buildConfig(configRoot *store.TreeNode) map[string]map[string]json.RawMessage {
	if configRoot == nil {
		return nil
	}

	out := make(map[string]map[string]json.RawMessage)

	for _, clusterGroupID := range sortedChildNames(configRoot) {
		if clusterGroupID == "_meta" {
			continue
		}

		clusterGroupNode := child(configRoot, clusterGroupID)
		if clusterGroupNode == nil {
			continue
		}

		out[clusterGroupID] = make(map[string]json.RawMessage)

		for _, managementGroupID := range sortedChildNames(clusterGroupNode) {
			out[clusterGroupID][managementGroupID] = valueOf(child(clusterGroupNode, managementGroupID))
		}
	}

	if len(out) == 0 {
		return nil
	}

	return out
}

func buildFlatValueMap(root *store.TreeNode) map[string]json.RawMessage {
	if root == nil {
		return nil
	}

	out := make(map[string]json.RawMessage)

	for _, name := range sortedChildNames(root) {
		value := valueOf(child(root, name))
		if value != nil {
			out[name] = value
		}
	}

	if len(out) == 0 {
		return nil
	}

	return out
}

func buildClusterGroup(
	clusterGroupID string,
	clusterGroupNode *store.TreeNode,
	config map[string]json.RawMessage,
	meta model.DynamicConfigDocument,
) *ClusterGroupView {
	groupMeta := meta.ClusterGroups[clusterGroupID]

	out := &ClusterGroupView{
		ID:               clusterGroupID,
		Name:             nameOrID(groupMeta.Name, clusterGroupID),
		ManagementGroups: make(map[string]*ManagementGroupView),
	}

	for _, managementGroupID := range sortedChildNames(clusterGroupNode) {
		if isManagementSystemKey(managementGroupID) {
			continue
		}

		managementGroupNode := child(clusterGroupNode, managementGroupID)
		if managementGroupNode == nil {
			continue
		}

		out.ManagementGroups[managementGroupID] = buildManagementGroup(
			managementGroupID,
			managementGroupNode,
			config[managementGroupID],
			meta,
		)
	}

	return out
}

func buildManagementGroup(
	managementGroupID string,
	managementGroupNode *store.TreeNode,
	config json.RawMessage,
	meta model.DynamicConfigDocument,
) *ManagementGroupView {
	group := &ManagementGroupView{
		ID:      managementGroupID,
		Name:    managementGroupID,
		Config:  config,
		Desired: valueOf(child(managementGroupNode, "desired")),
		Actual:  ensureDetails(valueOf(child(managementGroupNode, "actual"))),
		Health:  ensureDetails(valueOf(child(managementGroupNode, "health"))),
	}

	nodes := make(map[string]*NodeView)

	for _, nodeID := range sortedChildNames(managementGroupNode) {
		if isManagementSystemKey(nodeID) {
			continue
		}

		node := child(managementGroupNode, nodeID)
		if node == nil {
			continue
		}

		if looksLikeRoleContainer(node) {
			nodes[nodeID] = buildNode(nodeID, node, meta)
		}
	}

	if looksLikeRoleContainer(managementGroupNode) {
		nodes[managementGroupID] = buildNode(managementGroupID, managementGroupNode, meta)
	}

	if len(nodes) > 0 {
		group.Nodes = nodes
	}

	return group
}

func buildNode(nodeID string, node *store.TreeNode, meta model.DynamicConfigDocument) *NodeView {
	nodeMeta := meta.Nodes[nodeID]

	out := &NodeView{
		ID:   nodeID,
		Name: nameOrID(nodeMeta.Name, nodeID),
	}

	roles := make(map[string]*RoleView)

	for _, roleID := range sortedChildNames(node) {
		if isManagementSystemKey(roleID) {
			continue
		}

		role := child(node, roleID)
		if role == nil {
			continue
		}

		roleMeta := meta.Roles[roleID]

		roleView := &RoleView{
			ID:     roleID,
			Name:   nameOrID(roleMeta.Name, roleID),
			Actual: ensureDetails(valueOf(child(role, "actual"))),
			Health: ensureDetails(valueOf(child(role, "health"))),
		}

		if roleView.Actual != nil || roleView.Health != nil {
			roles[roleID] = roleView
		}
	}

	if len(roles) > 0 {
		out.Roles = roles
	}

	return out
}

func ensureDetails(raw json.RawMessage) json.RawMessage {
	if raw == nil {
		return nil
	}

	var obj map[string]interface{}
	if err := json.Unmarshal(raw, &obj); err != nil {
		return raw
	}

	if _, ok := obj["details"]; !ok {
		obj["details"] = ""
	}

	out, err := json.Marshal(obj)
	if err != nil {
		return raw
	}

	return out
}

func looksLikeRoleContainer(node *store.TreeNode) bool {
	if node == nil {
		return false
	}

	for _, name := range sortedChildNames(node) {
		if isManagementSystemKey(name) {
			continue
		}

		role := child(node, name)
		if role == nil {
			continue
		}

		if child(role, "actual") != nil || child(role, "health") != nil {
			return true
		}
	}

	return false
}

func valueOf(node *store.TreeNode) json.RawMessage {
	if node == nil || len(node.Value) == 0 {
		return nil
	}

	out := make(json.RawMessage, len(node.Value))
	copy(out, node.Value)

	return out
}

func child(node *store.TreeNode, name string) *store.TreeNode {
	if node == nil || node.Children == nil {
		return nil
	}

	return node.Children[name]
}

func sortedChildNames(node *store.TreeNode) []string {
	if node == nil || node.Children == nil {
		return nil
	}

	names := make([]string, 0, len(node.Children))
	for name := range node.Children {
		names = append(names, name)
	}

	sort.Strings(names)

	return names
}

func nameOrID(name string, id string) string {
	if name != "" {
		return name
	}

	return id
}

func isRootSystemKey(name string) bool {
	switch name {
	case "leadership", "commands", "commands_history", "registry", "session", "config":
		return true
	default:
		return false
	}
}

func isManagementSystemKey(name string) bool {
	switch name {
	case "desired", "actual", "health":
		return true
	default:
		return false
	}
}
