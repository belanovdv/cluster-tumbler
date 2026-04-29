package keys

import "path"

func Root(clusterID string) string {
	return path.Join("/", clusterID, "cluster")
}

func Leadership(clusterID string) string {
	return path.Join(Root(clusterID), "leadership")
}

func RegistryRoot(clusterID string) string {
	return path.Join(Root(clusterID), "registry")
}

func Registry(clusterID, nodeID string) string {
	return path.Join(RegistryRoot(clusterID), nodeID)
}

func SessionRoot(clusterID string) string {
	return path.Join(Root(clusterID), "session")
}

func Session(clusterID, nodeID string) string {
	return path.Join(SessionRoot(clusterID), nodeID)
}

func ConfigRoot(clusterID string) string {
	return path.Join(Root(clusterID), "config")
}

func ConfigMeta(clusterID string) string {
	return path.Join(ConfigRoot(clusterID), "_meta")
}

func ManagementGroupConfig(clusterID, clusterGroup, managementGroup string) string {
	return path.Join(ConfigRoot(clusterID), clusterGroup, managementGroup)
}

func ClusterGroup(clusterID, clusterGroup string) string {
	return path.Join(Root(clusterID), clusterGroup)
}

func ManagementGroup(clusterID, clusterGroup, managementGroup string) string {
	return path.Join(ClusterGroup(clusterID, clusterGroup), managementGroup)
}

func Desired(clusterID, clusterGroup, managementGroup string) string {
	return path.Join(ManagementGroup(clusterID, clusterGroup, managementGroup), "desired")
}

func Actual(clusterID, clusterGroup, managementGroup string) string {
	return path.Join(ManagementGroup(clusterID, clusterGroup, managementGroup), "actual")
}

func Health(clusterID, clusterGroup, managementGroup string) string {
	return path.Join(ManagementGroup(clusterID, clusterGroup, managementGroup), "health")
}

func Node(clusterID, clusterGroup, managementGroup, nodeID string) string {
	mg := ManagementGroup(clusterID, clusterGroup, managementGroup)
	if managementGroup == nodeID {
		return mg
	}

	return path.Join(mg, nodeID)
}

func Role(clusterID, clusterGroup, managementGroup, nodeID, role string) string {
	return path.Join(Node(clusterID, clusterGroup, managementGroup, nodeID), role)
}

func RoleActual(clusterID, clusterGroup, managementGroup, nodeID, role string) string {
	return path.Join(Role(clusterID, clusterGroup, managementGroup, nodeID, role), "actual")
}

func RoleHealth(clusterID, clusterGroup, managementGroup, nodeID, role string) string {
	return path.Join(Role(clusterID, clusterGroup, managementGroup, nodeID, role), "health")
}

func Commands(clusterID string) string {
	return path.Join(Root(clusterID), "commands")
}

func Command(clusterID, commandID string) string {
	return path.Join(Commands(clusterID), commandID)
}

func CommandHistory(clusterID, commandID string) string {
	return path.Join(Root(clusterID), "commands_history", commandID)
}
