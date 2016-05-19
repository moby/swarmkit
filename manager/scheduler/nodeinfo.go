package scheduler

import "github.com/docker/swarm-v2/api"

// NodeInfo contains a node and some additional metadata.
type NodeInfo struct {
	*api.Node
	Tasks              map[string]*api.Task
	AvailableResources api.Resources
}

func newNodeInfo(n *api.Node, tasks map[string]*api.Task, availableResources api.Resources) NodeInfo {
	nodeInfo := NodeInfo{
		Node:               n,
		Tasks:              make(map[string]*api.Task),
		AvailableResources: availableResources,
	}

	for _, t := range tasks {
		nodeInfo.addTask(t)
	}
	return nodeInfo
}

func (nodeInfo *NodeInfo) removeTask(t *api.Task) bool {
	if nodeInfo.Tasks == nil || nodeInfo.Node == nil {
		return false
	}
	if _, ok := nodeInfo.Tasks[t.ID]; !ok {
		return false
	}

	delete(nodeInfo.Tasks, t.ID)
	if t.GetContainer() != nil {
		specContainer := t.GetContainer().Spec
		reservations := taskReservations(&specContainer)
		nodeInfo.AvailableResources.MemoryBytes += reservations.MemoryBytes
		nodeInfo.AvailableResources.NanoCPUs += reservations.NanoCPUs
	}

	return true
}

func (nodeInfo *NodeInfo) addTask(t *api.Task) bool {
	if nodeInfo.Node == nil {
		return false
	}
	if nodeInfo.Tasks == nil {
		nodeInfo.Tasks = make(map[string]*api.Task)
	}
	container := t.GetContainer()
	if _, ok := nodeInfo.Tasks[t.ID]; !ok {
		nodeInfo.Tasks[t.ID] = t
		if container != nil {
			specContainer := container.Spec
			reservations := taskReservations(&specContainer)
			nodeInfo.AvailableResources.MemoryBytes -= reservations.MemoryBytes
			nodeInfo.AvailableResources.NanoCPUs -= reservations.NanoCPUs
		}
		return true
	}

	return false
}

func taskReservations(container *api.ContainerSpec) (reservations api.Resources) {
	if container != nil && container.Resources != nil && container.Resources.Reservations != nil {
		reservations = *container.Resources.Reservations
	}
	return
}
