// Package config загружает YAML-конфигурацию cluster-tumbler.
package config

import (
	"os"

	"gopkg.in/yaml.v3"
)

// Config — корневая конфигурация.
type Config struct {
	Cluster ClusterConfig `yaml:"cluster"`
	Agent   AgentConfig   `yaml:"agent"`
	Etcd    EtcdConfig    `yaml:"etcd"`
	HTTP    HTTPConfig    `yaml:"http"`
	Logging LoggingConfig `yaml:"logging"`
}

type ClusterConfig struct {
	ID string `yaml:"id"`
}

type AgentConfig struct {
	NodeID      string       `yaml:"node_id"`
	Memberships []Membership `yaml:"memberships"`
}

type Membership struct {
	ClusterGroup            string   `yaml:"cluster_group"`
	ManagementGroup         string   `yaml:"management_group"`
	ManagementGroupPriority int      `yaml:"management_group_priority"`
	Roles                   []string `yaml:"roles"`
}

type EtcdConfig struct {
	Endpoints []string `yaml:"endpoints"`
}

type HTTPConfig struct {
	Listen string `yaml:"listen"`
}

type LoggingConfig struct {
	Level   string `yaml:"level"`
	Format  string `yaml:"format"`
	Console bool   `yaml:"console"`
}

// Load читает YAML файл конфигурации.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}
