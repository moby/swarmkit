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

func (nodeInfo *NodeInfo) removeTask(t *api.Task) {
	reservations := taskReservations(t)
	if nodeInfo.Tasks != nil {
		delete(nodeInfo.Tasks, t.ID)
	}
	nodeInfo.AvailableResources.MemoryBytes += reservations.MemoryBytes
	nodeInfo.AvailableResources.NanoCPUs += reservations.NanoCPUs
}

func (nodeInfo *NodeInfo) addTask(t *api.Task) {
	reservations := taskReservations(t)
	if nodeInfo.Tasks == nil {
		nodeInfo.Tasks = make(map[string]*api.Task)
	}
	nodeInfo.Tasks[t.ID] = t
	nodeInfo.AvailableResources.MemoryBytes -= reservations.MemoryBytes
	nodeInfo.AvailableResources.NanoCPUs -= reservations.NanoCPUs
}

func taskReservations(t *api.Task) (reservations api.Resources) {
	container := t.Spec.GetContainer()
	if container != nil && container.Resources != nil && container.Resources.Reservations != nil {
		reservations = *container.Resources.Reservations
	}
	return
}
