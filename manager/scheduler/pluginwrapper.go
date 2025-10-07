package scheduler

import (
	"github.com/moby/swarmkit/v2/api"
	"github.com/moby/swarmkit/v2/manager/scheduler/common"
)

// PluginAdapter adapts a PlacementPlugin to the common.PluginInterface
type PluginAdapter struct {
	plugin PlacementPlugin
	name   string
}

// NewPluginAdapter creates a new adapter for a placement plugin
func NewPluginAdapter(name string, plugin PlacementPlugin) *PluginAdapter {
	return &PluginAdapter{
		plugin: plugin,
		name:   name,
	}
}

// Name returns the plugin name
func (p *PluginAdapter) Name() string {
	return p.name
}

// Schedule implements the common.PluginInterface
func (p *PluginAdapter) Schedule(task *api.Task, nodes []common.NodeInfo) (common.NodeInfo, error) {
	// Call the underlying plugin
	filtered, err := p.plugin.FilterNodes(task, nodes)
	if err != nil {
		return common.NodeInfo{}, err
	}
	
	// If we have results, return the first one
	if len(filtered) > 0 {
		return filtered[0], nil
	}
	
	return common.NodeInfo{}, nil
}
