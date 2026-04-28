// Package keys описывает схему ETCD-ключей cluster-tumbler.
package keys

import "path"

// Root возвращает корень дерева cluster-tumbler в ETCD.
func Root(clusterID string) string {
	return path.Join("/", clusterID, "cluster")
}

// Leadership возвращает ключ leader election.
func Leadership(clusterID string) string {
	return path.Join(Root(clusterID), "leadership")
}

// RegistryRoot возвращает корень глобальной регистрации агентов.
func RegistryRoot(clusterID string) string {
	return path.Join(Root(clusterID), "registry")
}

// Registry возвращает ключ регистрации физического агента.
func Registry(clusterID, nodeID string) string {
	return path.Join(RegistryRoot(clusterID), nodeID)
}

// SessionRoot возвращает корень session lease агентов.
func SessionRoot(clusterID string) string {
	return path.Join(Root(clusterID), "session")
}

// Session возвращает session ключ физического агента.
func Session(clusterID, nodeID string) string {
	return path.Join(SessionRoot(clusterID), nodeID)
}

// ConfigRoot возвращает корень глобальной конфигурации кластера.
func ConfigRoot(clusterID string) string {
	return path.Join(Root(clusterID), "config")
}

// ManagementGroupConfig возвращает ключ конфигурации management group.
func ManagementGroupConfig(clusterID, clusterGroup, managementGroup string) string {
	return path.Join(ConfigRoot(clusterID), clusterGroup, managementGroup)
}

// ClusterGroup возвращает путь cluster group.
func ClusterGroup(clusterID, clusterGroup string) string {
	return path.Join(Root(clusterID), clusterGroup)
}

// ManagementGroup возвращает путь management group.
func ManagementGroup(clusterID, clusterGroup, managementGroup string) string {
	return path.Join(ClusterGroup(clusterID, clusterGroup), managementGroup)
}

// Desired возвращает desired ключ management group.
func Desired(clusterID, clusterGroup, managementGroup string) string {
	return path.Join(ManagementGroup(clusterID, clusterGroup, managementGroup), "desired")
}

// Actual возвращает aggregate actual ключ management group.
func Actual(clusterID, clusterGroup, managementGroup string) string {
	return path.Join(ManagementGroup(clusterID, clusterGroup, managementGroup), "actual")
}

// Health возвращает aggregate health ключ management group.
func Health(clusterID, clusterGroup, managementGroup string) string {
	return path.Join(ManagementGroup(clusterID, clusterGroup, managementGroup), "health")
}

// Node возвращает путь node subtree внутри management group.
// Для instance-сценариев managementGroup может совпадать с nodeID.
func Node(clusterID, clusterGroup, managementGroup, nodeID string) string {
	mg := ManagementGroup(clusterID, clusterGroup, managementGroup)
	if managementGroup == nodeID {
		return mg
	}
	return path.Join(mg, nodeID)
}

// Role возвращает путь роли.
func Role(clusterID, clusterGroup, managementGroup, nodeID, role string) string {
	return path.Join(Node(clusterID, clusterGroup, managementGroup, nodeID), role)
}

// RoleActual возвращает actual ключ роли.
func RoleActual(clusterID, clusterGroup, managementGroup, nodeID, role string) string {
	return path.Join(Role(clusterID, clusterGroup, managementGroup, nodeID, role), "actual")
}

// RoleHealth возвращает health ключ роли.
func RoleHealth(clusterID, clusterGroup, managementGroup, nodeID, role string) string {
	return path.Join(Role(clusterID, clusterGroup, managementGroup, nodeID, role), "health")
}

// Commands возвращает корень команд администратора.
func Commands(clusterID string) string {
	return path.Join(Root(clusterID), "commands")
}

// Command возвращает ключ конкретной команды.
func Command(clusterID, commandID string) string {
	return path.Join(Commands(clusterID), commandID)
}

// CommandHistory возвращает ключ истории команды.
func CommandHistory(clusterID, commandID string) string {
	return path.Join(Root(clusterID), "commands_history", commandID)
}
