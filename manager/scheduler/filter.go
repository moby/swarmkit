package scheduler

import "github.com/docker/swarm-v2/api"

// Filter checks whether the given task can run on the given node.
type Filter interface {
	// Enabled returns true when the filter is enabled for a given task.
	// For instance, a constraints filter would return `false` if the task doesn't contain any constraints.
	Enabled(*api.Task) bool

	// Check returns true if the task can be scheduled into the given node.
	// This function should not be called if the the filter is not Enabled.
	Check(*api.Task, *NodeInfo) bool
}

// ReadyFilter checks that the node is ready to schedule tasks.
type ReadyFilter struct {
}

// Enabled returns true when the filter is enabled for a given task.
func (f *ReadyFilter) Enabled(t *api.Task) bool {
	return true
}

// Check returns true if the task can be scheduled into the given node.
func (f *ReadyFilter) Check(t *api.Task, n *NodeInfo) bool {
	return n.Status.State == api.NodeStatus_READY &&
		n.Spec.Availability == api.NodeAvailabilityActive
}

// ResourceFilter checks that the node has enough resources available to run
// the task.
type ResourceFilter struct {
}

// Enabled returns true when the filter is enabled for a given task.
func (f *ResourceFilter) Enabled(t *api.Task) bool {
	if t.GetContainer() == nil {
		return false
	}

	c := t.GetContainer().Spec

	r := c.Resources
	if r == nil || r.Reservations == nil {
		return false
	}
	if r.Reservations.NanoCPUs == 0 && r.Reservations.MemoryBytes == 0 {
		return false
	}
	return true
}

// Check returns true if the task can be scheduled into the given node.
func (f *ResourceFilter) Check(t *api.Task, n *NodeInfo) bool {
	container := t.GetContainer().Spec
	if container.Resources == nil || container.Resources.Reservations == nil {
		return true
	}

	res := container.Resources.Reservations

	if res.NanoCPUs > n.AvailableResources.NanoCPUs {
		return false
	}

	if res.MemoryBytes > n.AvailableResources.MemoryBytes {
		return false
	}

	return true
}

// PortFilter checks that the node has the requested network ports available.
type PortFilter struct {
}

// Enabled returns true when the filter is enabled for a given task.
func (f *PortFilter) Enabled(t *api.Task) bool {
	if t.GetContainer() == nil {
		return false
	}

	c := t.GetContainer().Spec
	if len(c.ExposedPorts) == 0 {
		return false
	}
	return true
}

// Check returns true if the task can be scheduled into the given node.
func (f *PortFilter) Check(t *api.Task, n *NodeInfo) bool {
	if t.GetContainer() == nil {
		return false
	}

	container := t.GetContainer().Spec

	for _, ep := range container.ExposedPorts {
		if ep.HostPort == 0 {
			continue
		}
		if _, found := n.ReservedPorts[ep.HostPort]; found {
			return false
		}
	}

	return true
}
