package pluginscheduler

import (
	"fmt"

	"github.com/moby/swarmkit/v2/api"
	"github.com/moby/swarmkit/v2/manager/scheduler/common"
)

// Scheduler handles scheduling using plugins
type Scheduler struct {
	plugins map[string]common.PluginInterface
}

// New creates a new plugin scheduler
func New() *Scheduler {
	return &Scheduler{
		plugins: make(map[string]common.PluginInterface),
	}
}

// RegisterPlugin registers a plugin with the scheduler
func (s *Scheduler) RegisterPlugin(p common.PluginInterface) error {
	if p == nil {
		return fmt.Errorf("cannot register nil plugin")
	}
	name := p.Name()
	if name == "" {
		return fmt.Errorf("plugin name cannot be empty")
	}
	s.plugins[name] = p
	return nil
}

// Schedule runs the scheduling process using the specified plugin
func (s *Scheduler) Schedule(pluginName string, task *api.Task, nodes []common.NodeInfo) (common.NodeInfo, error) {
	p, exists := s.plugins[pluginName]
	if !exists {
		return common.NodeInfo{}, fmt.Errorf("plugin %s not found", pluginName)
	}
	return p.Schedule(task, nodes)
}

// GetPlugin returns a plugin by name
func (s *Scheduler) GetPlugin(name string) common.PluginInterface {
	return s.plugins[name]
}
