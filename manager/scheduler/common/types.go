package common

import (
	"github.com/moby/swarmkit/v2/api"
)

// NodeInfo contains a node and its available resources
type NodeInfo struct {
	Node               *api.Node
	Tasks              map[string]*api.Task
	AvailableResources *api.Resources
	// Simplified version of NodeInfo that avoids circular dependencies
}

// NewNodeInfo creates a new NodeInfo instance
func NewNodeInfo(node *api.Node, tasks map[string]*api.Task, resources *api.Resources) NodeInfo {
	return NodeInfo{
		Node:               node,
		Tasks:              tasks,
		AvailableResources: resources,
	}
}

// PluginInterface defines the scheduler plugin interface in a common package to avoid import cycles
type PluginInterface interface {
	// Name returns the name of the plugin
	Name() string
	// Schedule allows the plugin to select a node for the task
	Schedule(task *api.Task, nodeSet []NodeInfo) (NodeInfo, error)
}

// PluginScheduler defines the interface for plugin management
type PluginScheduler interface {
	// RegisterPlugin registers a plugin with the scheduler
	RegisterPlugin(p PluginInterface) error
	// Schedule runs the scheduling process using the specified plugin
	Schedule(pluginName string, task *api.Task, nodes []NodeInfo) (NodeInfo, error)
	// GetPlugin returns a plugin by name
	GetPlugin(name string) PluginInterface
}
