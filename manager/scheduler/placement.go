package scheduler

import (
	"sync"

	"github.com/moby/swarmkit/v2/api"
)

// PlacementPlugin defines the interface for external placement plugins.
type PlacementPlugin interface {
	// Name returns the name of the plugin.
	Name() string
	// FilterNodes allows the plugin to filter and rank nodes for a given task.
	FilterNodes(task *api.Task, nodes []NodeInfo) ([]NodeInfo, error)
}

var (
	plugins     = make(map[string]PlacementPlugin)
	pluginsLock sync.RWMutex
)

// RegisterPlacementPlugin registers a placement plugin for use by the scheduler
func RegisterPlacementPlugin(p PlacementPlugin) {
	if p == nil {
		return
	}
	pluginsLock.Lock()
	defer pluginsLock.Unlock()
	plugins[p.Name()] = p
}

// GetPlacementPlugin returns a placement plugin by name, or nil if no plugin
// with the given name is registered.
func GetPlacementPlugin(name string) PlacementPlugin {
	pluginsLock.RLock()
	defer pluginsLock.RUnlock()
	return plugins[name]
}
