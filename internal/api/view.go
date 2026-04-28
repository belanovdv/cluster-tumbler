package api

import (
	"encoding/json"
	"sort"

	"cluster-agent/internal/store"
)

type StateView struct {
	Ready    bool         `json:"ready"`
	Revision int64        `json:"revision"`
	Cluster  *ClusterView `json:"cluster"`
}

type ClusterView struct {
	ID         string                                `json:"id"`
	Leadership json.RawMessage                       `json:"leadership,omitempty"`
	Config     map[string]map[string]json.RawMessage `json:"config,omitempty"`
	Registry   map[string]json.RawMessage            `json:"registry,omitempty"`
	Session    map[string]json.RawMessage            `json:"session,omitempty"`
	Groups     map[string]*ClusterGroupView          `json:"groups"`
}

type ClusterGroupView struct {
	ManagementGroups map[string]*ManagementGroupView `json:"management_groups"`
}

type ManagementGroupView struct {
	Config  json.RawMessage     `json:"config,omitempty"`
	Desired json.RawMessage     `json:"desired,omitempty"`
	Actual  json.RawMessage     `json:"actual,omitempty"`
	Health  json.RawMessage     `json:"health,omitempty"`
	Nodes   map[string]*NodeView `json:"nodes,omitempty"`
}

type NodeView struct {
	Roles map[string]*RoleView `json:"roles,omitempty"`
}

type RoleView struct {
	Actual json.RawMessage `json:"actual,omitempty"`
	Health json.RawMessage `json:"health,omitempty"`
}

func BuildStateView(clusterID string, ready bool, revision int64, root *store.TreeNode) StateView {
	view := StateView{
		Ready:    ready,
		Revision: revision,
		Cluster: &ClusterView{
			ID:     clusterID,
			Groups: make(map[string]*ClusterGroupView),
		},
	}

	clusterRoot := child(root, clusterID)
	clusterNode := child(clusterRoot, "cluster")
	if clusterNode == nil {
		return view
	}

	if leadership := child(clusterNode, "leadership"); leadership != nil {
		view.Cluster.Leadership = valueOf(leadership)
	}

	view.Cluster.Config = buildConfig(child(clusterNode, "config"))
	view.Cluster.Registry = buildFlatValueMap(child(clusterNode, "registry"))
	view.Cluster.Session = buildFlatValueMap(child(clusterNode, "session"))

	for _, clusterGroupName := range sortedChildNames(clusterNode) {
		if isRootSystemKey(clusterGroupName) {
			continue
		}

		clusterGroupNode := child(clusterNode, clusterGroupName)
		if clusterGroupNode == nil {
			continue
		}

		view.Cluster.Groups[clusterGroupName] = buildClusterGroup(
			clusterGroupNode,
			view.Cluster.Config[clusterGroupName],
		)
	}

	return view
}

func buildConfig(configRoot *store.TreeNode) map[string]map[string]json.RawMessage {
	if configRoot == nil {
		return nil
	}

	out := make(map[string]map[string]json.RawMessage)

	for _, clusterGroupName := range sortedChildNames(configRoot) {
		clusterGroupNode := child(configRoot, clusterGroupName)
		if clusterGroupNode == nil {
			continue
		}

		out[clusterGroupName] = make(map[string]json.RawMessage)

		for _, managementGroupName := range sortedChildNames(clusterGroupNode) {
			out[clusterGroupName][managementGroupName] = valueOf(child(clusterGroupNode, managementGroupName))
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
	clusterGroupNode *store.TreeNode,
	config map[string]json.RawMessage,
) *ClusterGroupView {
	out := &ClusterGroupView{
		ManagementGroups: make(map[string]*ManagementGroupView),
	}

	for _, managementGroupName := range sortedChildNames(clusterGroupNode) {
		if isManagementSystemKey(managementGroupName) {
			continue
		}

		managementGroupNode := child(clusterGroupNode, managementGroupName)
		if managementGroupNode == nil {
			continue
		}

		out.ManagementGroups[managementGroupName] = buildManagementGroup(
			managementGroupName,
			managementGroupNode,
			config[managementGroupName],
		)
	}

	return out
}

func buildManagementGroup(
	managementGroupName string,
	managementGroupNode *store.TreeNode,
	config json.RawMessage,
) *ManagementGroupView {
	group := &ManagementGroupView{
		Config:  config,
		Desired: valueOf(child(managementGroupNode, "desired")),
		Actual:  ensureDetails(valueOf(child(managementGroupNode, "actual"))),
		Health:  ensureDetails(valueOf(child(managementGroupNode, "health"))),
	}

	nodes := make(map[string]*NodeView)

	for _, nodeName := range sortedChildNames(managementGroupNode) {
		if isManagementSystemKey(nodeName) {
			continue
		}

		node := child(managementGroupNode, nodeName)
		if node == nil {
			continue
		}

		if looksLikeRoleContainer(node) {
			nodes[nodeName] = buildNode(node)
		}
	}

	if looksLikeRoleContainer(managementGroupNode) {
		nodes[managementGroupName] = buildNode(managementGroupNode)
	}

	if len(nodes) > 0 {
		group.Nodes = nodes
	}

	return group
}

func buildNode(node *store.TreeNode) *NodeView {
	out := &NodeView{}
	roles := make(map[string]*RoleView)

	for _, roleName := range sortedChildNames(node) {
		if isManagementSystemKey(roleName) {
			continue
		}

		role := child(node, roleName)
		if role == nil {
			continue
		}

		roleView := &RoleView{
			Actual: ensureDetails(valueOf(child(role, "actual"))),
			Health: ensureDetails(valueOf(child(role, "health"))),
		}

		if roleView.Actual != nil || roleView.Health != nil {
			roles[roleName] = roleView
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


