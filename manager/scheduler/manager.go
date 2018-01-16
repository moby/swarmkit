package scheduler

import (
	"time"

	"github.com/docker/swarmkit/api"
	"github.com/docker/swarmkit/api/genericresource"
	"github.com/docker/swarmkit/log"
	"github.com/docker/swarmkit/manager/state"
	"github.com/docker/swarmkit/manager/state/store"
	"github.com/docker/swarmkit/protobuf/ptypes"
	"golang.org/x/net/context"
)

func (s *Scheduler) ScheduleManager(ctx context.Context, t *api.Task, managers *api.Node, workers *api.Node) {
  s.setupTasksList()
	s.nodeSet.alloc((len(workers))+(len(managers)))
  schedulingDecisions := map[string]schedulingDecision
  taskGroup := map[string]*api.Task
  taskGroup[0] = t


  // spoof running managers as running tasks
  for _, n := range managers {
		var resources api.Resources
		if n.Description != nil && n.Description.Resources != nil {
			resources = *n.Description.Resources
		}
    // task spoof
    ts := t
    ts.NodeID := n.ID
    ts.Status.State := api.TaskStateRunning
    tasksByNode[ts.NodeID][ts.ID] = ts
    }


    s.nodeSet.addOrUpdateNode(newNodeInfo(n, tasksByNode[n.ID], resources))


  for _, n := range workers {
    var resources api.Resources
    if n.Description != nil && n.Description.Resources != nil {
      resources = *n.Description.Resources
    }
    s.nodeSet.addOrUpdateNode(newNodeInfo(n, tasksByNode[n.ID], resources))
  }

	s.pipeline.SetTask(t)

	now := time.Now()

	nodeLess := func(a *NodeInfo, b *NodeInfo) bool {
		// Total number of tasks breaks ties.
		return a.ActiveTasksCount < b.ActiveTasksCount
	}

	var prefs []*api.PlacementPreference
	if t.Spec.Placement != nil {
		prefs = t.Spec.Placement.Preferences
	}

  tree := s.nodeSet.tree(t.ServiceID, prefs, len(taskGroup), s.pipeline.Process, nodeLess)

  s.scheduleNTasksOnSubtree(ctx, len(taskGroup), taskGroup, &tree, schedulingDecisions, nodeLess)
	if len(taskGroup) != 0 {
		s.noSuitableNode(ctx, taskGroup, schedulingDecisions)
	}

  targetNodeID := schedulingDecisions[0].new.NodeID

  err := s.store.Update(func(tx store.Tx) error {

      updatedNode := store.GetNode(tx, targetNodeID)
      if updatedNode == nil || updatedNode.Spec.DesiredRole != node.Spec.DesiredRole || updatedNode.Role != node.Role {
        return nil
      }
      updatedNode.Spec.DesiredRole = api.NodeRoleManager
      return store.UpdateNode(tx, updatedNode)

  }


}
