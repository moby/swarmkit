package scheduler

import "github.com/docker/swarm-v2/api"

// NodeInfo contains a node and some additional metadata.
type NodeInfo struct {
	*api.Node
	Tasks              map[string]*api.Task
	AvailableResources api.Resources

	// There are some corner cases where we may assign multiple tasks that
	// need the same port to a single node. To deal with this properly,
	// ReservedPorts has a set of tasks using each port rather than just a
	// set of ports.
	ReservedPorts map[uint32]map[string]struct{}
}

func newNodeInfo(n *api.Node, tasks map[string]*api.Task, availableResources api.Resources) NodeInfo {
	nodeInfo := NodeInfo{
		Node:               n,
		Tasks:              make(map[string]*api.Task),
		AvailableResources: availableResources,
		ReservedPorts:      make(map[uint32]map[string]struct{}),
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

		for _, ep := range specContainer.ExposedPorts {
			if ep.HostPort == 0 {
				continue
			}
			if nodeInfo.ReservedPorts[ep.HostPort] != nil {
				delete(nodeInfo.ReservedPorts[ep.HostPort], t.ID)
				if len(nodeInfo.ReservedPorts[ep.HostPort]) == 0 {
					delete(nodeInfo.ReservedPorts, ep.HostPort)
				}
			}
		}
	}
	if statusContainer := t.Status.GetContainer(); statusContainer != nil {
		for _, ep := range statusContainer.ExposedPorts {
			if ep.HostPort == 0 {
				continue
			}
			if nodeInfo.ReservedPorts[ep.HostPort] != nil {
				delete(nodeInfo.ReservedPorts[ep.HostPort], t.ID)
				if len(nodeInfo.ReservedPorts[ep.HostPort]) == 0 {
					delete(nodeInfo.ReservedPorts, ep.HostPort)
				}
			}
		}

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

			for _, ep := range specContainer.ExposedPorts {
				if ep.HostPort == 0 {
					continue
				}
				if _, found := nodeInfo.ReservedPorts[ep.HostPort]; !found {
					nodeInfo.ReservedPorts[ep.HostPort] = make(map[string]struct{})
				}
				if _, found := nodeInfo.ReservedPorts[ep.HostPort][t.ID]; !found {
					nodeInfo.ReservedPorts[ep.HostPort][t.ID] = struct{}{}
				}
			}
		}
		return true
	}

	// If a task is not new to this node, it could have a new port
	// allocated.
	changes := false
	if statusContainer := t.Status.GetContainer(); statusContainer != nil {
		for _, ep := range statusContainer.ExposedPorts {
			if ep.HostPort == 0 {
				continue
			}
			if nodeInfo.ReservedPorts == nil {
				nodeInfo.ReservedPorts = make(map[uint32]map[string]struct{})
			}
			if _, found := nodeInfo.ReservedPorts[ep.HostPort]; !found {
				nodeInfo.ReservedPorts[ep.HostPort] = make(map[string]struct{})
			}
			if _, found := nodeInfo.ReservedPorts[ep.HostPort][t.ID]; !found {
				changes = true
				nodeInfo.ReservedPorts[ep.HostPort][t.ID] = struct{}{}
			}
		}
	}
	return changes
}

func taskReservations(container *api.ContainerSpec) (reservations api.Resources) {
	if container != nil && container.Resources != nil && container.Resources.Reservations != nil {
		reservations = *container.Resources.Reservations
	}
	return
}
