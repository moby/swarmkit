package volumeallocator

import (
	"github.com/docker/swarm-v2/api"
)

// VolumeAllocator acts as the controller for all cluster-wide volume
// operations: creating and deleting volumes
// Cluster-wide volumes are simpler than Networks. We have still implemented
// a VolumeAllocator because:
// 1. It architecturally aligns with the NetworkAllocator design
// 2. It allows us to extend support for cluster-wide volumes in the future
type VolumeAllocator struct {
	// Cache of cluster-wide volumes, indexed by volume name
	clusterVolumes map[string]*api.Volume

	// Which tasks have been fully allocated
	allocatedTasks map[string]struct{}
}

// New returns a new VolumeAllocator handle
func New() (*VolumeAllocator, error) {
	va := &VolumeAllocator{
		clusterVolumes: make(map[string]*api.Volume),
		allocatedTasks: make(map[string]struct{}),
	}

	return va, nil
}

// FindClusterVolume finds a volume from cached state
func (va *VolumeAllocator) FindClusterVolume(VolumeName string) *api.Volume {
	val, ok := va.clusterVolumes[VolumeName]
	if !ok {
		return nil
	}
	return val
}

// AddVolumeToCache - adds the volume object to the Cache
func (va *VolumeAllocator) AddVolumeToCache(v *api.Volume) {
	va.clusterVolumes[v.Spec.Annotations.Name] = v
}

// RemoveVolumeFromCache - Removes a volume from the cache
func (va *VolumeAllocator) RemoveVolumeFromCache(name string) {
	delete(va.clusterVolumes, name)
}

// AllocateTask marks a task as allocated
func (va *VolumeAllocator) AllocateTask(t *api.Task) {
	var a struct{}
	va.allocatedTasks[t.ID] = a
}

// DeallocateTask releases all the resources for all the
// volume that a task is attached to.
// It is safe to call DeallocateTask even if the Task is not allocated
func (va *VolumeAllocator) DeallocateTask(t *api.Task) {
	delete(va.allocatedTasks, t.ID)
}

// IsTaskAllocated returns if the passed task has it's volume resources allocated or not.
func (va *VolumeAllocator) IsTaskAllocated(t *api.Task) bool {
	// If the Task has already been, look up pre-computed info and return it
	_, ok := va.allocatedTasks[t.ID]
	if ok {
		return true
	}
	return false
}
