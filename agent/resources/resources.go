package resources

import (
	"sync"

	"github.com/docker/swarmkit/agent/exec"
	"github.com/docker/swarmkit/api"
)

// resources is a map that keeps all the currently available resources to the agent
// mapped by resource ID.
type resources struct {
	mu sync.RWMutex
	m  map[string]*api.Resource
}

// NewManager returns a place to store resources.
func NewManager() exec.ResourcesManager {
	return &resources{
		m: make(map[string]*api.Resource),
	}
}

// Get returns a resource by ID.  If the resource doesn't exist, returns nil.
func (r *resources) Get(resourceID string) *api.Resource {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r, ok := r.m[resourceID]; ok {
		return r
	}
	return nil
}

// Add adds one or more resources to the resource map.
func (r *resources) Add(resources ...api.Resource) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, resource := range resources {
		r.m[resource.ID] = resource.Copy()
	}
}

// Remove removes one or more resources by ID from the resource map. Succeeds
// whether or not the given IDs are in the map.
func (r *resources) Remove(resources []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, resource := range resources {
		delete(r.m, resource)
	}
}

// Reset removes all the resources.
func (r *resources) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.m = make(map[string]*api.Resource)
}

// taskRestrictedResourcesProvider restricts the ids to the task.
type taskRestrictedResourcesProvider struct {
	resources   exec.ResourceGetter
	resourceIDs map[string]struct{} // allow list of resource ids
}

func (sp *taskRestrictedResourcesProvider) Get(resourceID string) *api.Resource {
	if _, ok := sp.resourceIDs[resourceID]; !ok {
		return nil
	}

	return sp.resources.Get(resourceID)
}

// Restrict provides a getter that only allows access to the resources
// referenced by the task.
func Restrict(resources exec.ResourceGetter, t *api.Task) exec.ResourceGetter {
	rids := map[string]struct{}{}

	for _, resourceID := range t.Spec.ResourceReferences {
		rids[resourceID] = struct{}{}
	}

	return &taskRestrictedResourcesProvider{resources: resources, resourceIDs: rids}
}
