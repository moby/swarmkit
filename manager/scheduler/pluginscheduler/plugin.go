package pluginscheduler

import (
	"github.com/moby/swarmkit/v2/api"
	"github.com/moby/swarmkit/v2/manager/scheduler/common"
)

// Plugin represents a scheduler plugin
type Plugin struct {
	name      string
	scheduler common.PluginInterface
}

// NewPlugin creates a new scheduler plugin
func NewPlugin(name string, scheduler common.PluginInterface) *Plugin {
	return &Plugin{
		name:      name,
		scheduler: scheduler,
	}
}

// Name returns the name of the plugin
func (p *Plugin) Name() string {
	return p.name
}

// Schedule allows the plugin to select a node for the task
func (p *Plugin) Schedule(task *api.Task, nodeSet []common.NodeInfo) (common.NodeInfo, error) {
	return p.scheduler.Schedule(task, nodeSet)
}
