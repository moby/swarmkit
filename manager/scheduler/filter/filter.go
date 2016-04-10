package filter

import "github.com/docker/swarm-v2/api"

type Filter interface {
	// Enabled returns true when the filter is enabled for a given task.
	// For instance, a constraints filter would return `false` if the task doesn't contain any constraints.
	Enabled(*api.Task) bool

	// Check returns true if the task can be scheduled into the given node.
	// This function should not be called if the the filter is not Enabled.
	Check(*api.Task, *api.Node) bool
}

type ReadyFilter struct {
}

func (f *ReadyFilter) Enabled(t *api.Task) bool {
	return true
}

func (f *ReadyFilter) Check(t *api.Task, n *api.Node) bool {
	return n.Status.State == api.NodeStatus_READY &&
		(n.Spec == nil || n.Spec.Availability == api.NodeAvailabilityActive)
}

type ResourceFilter struct {
}

func (f *ResourceFilter) Enabled(t *api.Task) bool {
	c := t.Spec.GetContainer()
	if c == nil {
		return false
	}
	if r := c.Resources; r == nil || (r.Reservations.NanoCPUs == 0 && r.Reservations.MemoryBytes == 0) {
		return false
	}
	return true
}

func (f *ResourceFilter) Check(t *api.Task, n *api.Node) bool {
	res := t.Spec.GetContainer().Resources.Reservations

	// TODO(aluzzardi): Actually check adding this task to the node won't put it above its resource capacity.
	if res.NanoCPUs > n.Description.Resources.NanoCPUs {
		return false
	}

	if res.MemoryBytes > n.Description.Resources.MemoryBytes {
		return false
	}

	return true
}
