package scheduler

import "github.com/docker/swarm-v2/api"

// NodeInfo contains a node and some additional metadata.
type NodeInfo struct {
	*api.Node
	Tasks              map[string]*api.Task
	AvailableResources api.Resources
}

func newNodeInfo(n *api.Node, tasks map[string]*api.Task, availableResources api.Resources) NodeInfo {
	return NodeInfo{
		Node:               n,
		Tasks:              tasks,
		AvailableResources: availableResources,
	}
}

func (nodeInfo *NodeInfo) removeTask(t *api.Task) bool {
	if nodeInfo.Tasks != nil {
		if _, ok := nodeInfo.Tasks[t.ID]; ok {
			delete(nodeInfo.Tasks, t.ID)
			reservations := taskReservations(t)
			nodeInfo.AvailableResources.MemoryBytes += reservations.MemoryBytes
			nodeInfo.AvailableResources.NanoCPUs += reservations.NanoCPUs
			return true
		}
	}
	return false
}

func (nodeInfo *NodeInfo) addTask(t *api.Task) bool {
	if nodeInfo.Tasks == nil {
		nodeInfo.Tasks = make(map[string]*api.Task)
	}
	if _, ok := nodeInfo.Tasks[t.ID]; !ok {
		nodeInfo.Tasks[t.ID] = t
		reservations := taskReservations(t)
		nodeInfo.AvailableResources.MemoryBytes -= reservations.MemoryBytes
		nodeInfo.AvailableResources.NanoCPUs -= reservations.NanoCPUs
		return true
	}
	return false
}

func taskReservations(t *api.Task) (reservations api.Resources) {
	container := t.Spec.GetContainer()
	if container != nil && container.Resources != nil && container.Resources.Reservations != nil {
		reservations = *container.Resources.Reservations
	}
	return
}
