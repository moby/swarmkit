package scheduler

import (
	"fmt"

	"github.com/moby/swarmkit/v2/api"
	"github.com/moby/swarmkit/v2/manager/scheduler/common"
)

func (s *Scheduler) selectNodesForTask(task *api.Task, nodes []NodeInfo) ([]NodeInfo, error) {
	if s.PlacementPluginName != "" {
		if plug, ok := s.placementPlugins[s.PlacementPluginName]; ok {
			if plug == nil {
				return nil, fmt.Errorf("placement plugin %q not initialized", s.PlacementPluginName)
			}
			
			// Convert internal NodeInfo to common.NodeInfo
			commonNodes := make([]common.NodeInfo, len(nodes))
			for i, n := range nodes {
				commonNodes[i] = n.ToCommon()
			}
			
			result, err := plug.Schedule(task, commonNodes)
			if err != nil {
				return nil, err
			}
			
			// Convert back to internal NodeInfo
			resultNodes := make([]NodeInfo, 1)
			resultNodes[0] = FromCommon(result)
			return resultNodes, nil
		}
	}
	return nodes, nil
}
