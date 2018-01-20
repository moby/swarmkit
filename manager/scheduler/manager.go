package scheduler

import (
	"time"

	"github.com/docker/swarmkit/api"
	"github.com/docker/swarmkit/api/genericresource"
	"github.com/docker/swarmkit/log"
	"github.com/docker/swarmkit/manager/state"
	"github.com/docker/swarmkit/manager/state/raft"
	"github.com/docker/swarmkit/manager/state/raft/membership"
	"github.com/docker/swarmkit/manager/state/store"
	"github.com/docker/swarmkit/protobuf/ptypes"
	"golang.org/x/net/context"
)

func (s *Scheduler) setupManagerList(tx store.ReadTx) error {
	// spoof running managers as running tasks
	tasksByNode := make(map[string]map[string]*api.Task)
  for _, m := range s.raft.cluster.members {
    // task spoof
    t.Status.State := api.TaskStateRunning
    tasksByNode[m.NodeID][t.ID] = t
    }
		return s.buildNodeSet(tx, tasksByNode)
}

func (s *Scheduler) ScheduleManager(ctx context.Context, t *api.Task, raftNode *raft.Node) {
	s.raft := raftNode
	updates, cancel, err := store.ViewAndWatch(s.store, s.setupManagerList)
	if err != nil {
		log.G(ctx).WithError(err).Errorf("snapshot store update failed")
		return err
	}
	defer cancel()

  schedulingDecisions := map[string]schedulingDecision
  taskGroup := map[string]*api.Task
  taskGroup[0] = t

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

	for _, n := range schedulingDecisions {
		err := s.store.Update(func(tx store.Tx) error {
				updatedNode := store.GetNode(tx, n.new.NodeID)
				if updatedNode == nil || updatedNode.Role == api.NodeRoleManager {
					return nil
				}
				updatedNode.Spec.DesiredRole = api.NodeRoleManager
				store.UpdateNode(tx, updatedNode)
				break
		}
	}
}
