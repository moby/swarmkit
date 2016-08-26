package scheduler

import "github.com/docker/swarmkit/api"

// NodeInfo contains a node and some additional metadata.
type NodeInfo struct {
	*api.Node
	TasksByService     map[string]map[string]*api.Task
	AvailableResources api.Resources
}

func newNodeInfo(n *api.Node, tasks map[string]*api.Task, availableResources api.Resources) NodeInfo {
	nodeInfo := NodeInfo{
		Node:               n,
		TasksByService:     make(map[string]map[string]*api.Task),
		AvailableResources: availableResources,
	}

	for _, t := range tasks {
		nodeInfo.addTask(t)
	}
	return nodeInfo
}

func (nodeInfo *NodeInfo) removeTask(t *api.Task) bool {
	if nodeInfo.TasksByService == nil {
		return false
	}
	taskMap, ok := nodeInfo.TasksByService[t.ServiceID]
	if !ok {
		return false
	}
	if _, ok := taskMap[t.ID]; !ok {
		return false
	}

	delete(taskMap, t.ID)
	if len(taskMap) == 0 {
		delete(nodeInfo.TasksByService, t.ServiceID)
	}

	reservations := taskReservations(t.Spec)
	nodeInfo.AvailableResources.MemoryBytes += reservations.MemoryBytes
	nodeInfo.AvailableResources.NanoCPUs += reservations.NanoCPUs

	return true
}

func (nodeInfo *NodeInfo) addTask(t *api.Task) bool {
	if nodeInfo.TasksByService == nil {
		nodeInfo.TasksByService = make(map[string]map[string]*api.Task)
	}
	tasksMap, ok := nodeInfo.TasksByService[t.ServiceID]
	if !ok {
		tasksMap = make(map[string]*api.Task)
		nodeInfo.TasksByService[t.ServiceID] = tasksMap
	}
	if _, ok := tasksMap[t.ID]; !ok {
		tasksMap[t.ID] = t
		reservations := taskReservations(t.Spec)
		nodeInfo.AvailableResources.MemoryBytes -= reservations.MemoryBytes
		nodeInfo.AvailableResources.NanoCPUs -= reservations.NanoCPUs
		return true
	}

	return false
}

func taskReservations(spec api.TaskSpec) (reservations api.Resources) {
	if spec.Resources != nil && spec.Resources.Reservations != nil {
		reservations = *spec.Resources.Reservations
	}
	return
}
