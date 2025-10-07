package scheduler

import (
	"sync"

	"github.com/moby/swarmkit/v2/api"
	"github.com/moby/swarmkit/v2/manager/scheduler/common"
)

// PlacementPlugin defines the interface for external placement plugins.
type PlacementPlugin interface {
	// Name returns the name of the plugin.
	Name() string
	// FilterNodes allows the plugin to filter and rank nodes for a given task.
	FilterNodes(task *api.Task, nodes []common.NodeInfo) ([]common.NodeInfo, error)
}

var (
	plugins     = make(map[string]common.PluginInterface)
	pluginsLock sync.RWMutex
)

// RegisterPlacementPlugin registers a placement plugin for use by the scheduler
func RegisterPlacementPlugin(p common.PluginInterface) {
	if p == nil {
		return
	}
	pluginsLock.Lock()
	defer pluginsLock.Unlock()
	plugins[p.Name()] = p
}

// GetPlacementPlugin returns a placement plugin by name, or nil if no plugin
// with the given name is registered.
func GetPlacementPlugin(name string) common.PluginInterface {
	pluginsLock.RLock()
	defer pluginsLock.RUnlock()
	return plugins[name]
}
