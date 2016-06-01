package volumeallocator

import (
	"testing"

	"github.com/docker/swarm-v2/api"
	"github.com/stretchr/testify/assert"
)

func newVolumeAllocator(t *testing.T) *VolumeAllocator {
	va, err := New()
	assert.NoError(t, err)
	assert.NotNil(t, va)
	return va
}

// TestClusterVolumesCache
//   - InitClusterVolume with 2 entries: V1, V2
//   - Lookup V2
//   - Lookup entry that doesn't exist: V3
//   - Add new Volume entry V3
//   - Lookup V3
//   - Remove V2
//   - Lookup V2
func TestClusterVolumesCache(t *testing.T) {
	v1 := &api.Volume{
		ID: "Vol1",
		Spec: api.VolumeSpec{
			Annotations: api.Annotations{
				Name: "Vol1",
			},
			DriverConfiguration: &api.Driver{
				Name: "driver1",
			},
		},
	}
	v2 := &api.Volume{
		ID: "Vol2",
		Spec: api.VolumeSpec{
			Annotations: api.Annotations{
				Name: "Vol2",
			},
			DriverConfiguration: &api.Driver{
				Name: "driver1",
			},
		},
	}
	v3 := &api.Volume{
		ID: "Vol3",
		Spec: api.VolumeSpec{
			Annotations: api.Annotations{
				Name: "Vol3",
			},
			DriverConfiguration: &api.Driver{
				Name: "driver1",
			},
		},
	}
	va := newVolumeAllocator(t)
	va.AddVolumeToCache(v1)
	va.AddVolumeToCache(v2)
	assert.Equal(t, va.FindClusterVolume(v2.Spec.Annotations.Name), v2)
	assert.Nil(t, va.FindClusterVolume("Vol3"))
	va.AddVolumeToCache(v3)
	assert.Equal(t, va.FindClusterVolume(v3.Spec.Annotations.Name), v3)
	va.RemoveVolumeFromCache(v2.Spec.Annotations.Name)
	assert.Nil(t, va.FindClusterVolume("Vol2"))
}

// TestAllocateTask
//   - Allocate Tasks T1, T2
//   - Lookup T2
//   - Lookup entry that doesn't exist: T3
//   - Add new Volume entry T3
//   - Lookup T3
//   - Remove T2
//   - Lookup T2
func TestAllocatedTasksCache(t *testing.T) {
	t1 := &api.Task{
		ID: "Task1",
		ServiceAnnotations: api.Annotations{
			Name: "annotation",
		},
	}
	t2 := &api.Task{
		ID: "Task2",
		ServiceAnnotations: api.Annotations{
			Name: "annotation",
		},
	}
	t3 := &api.Task{
		ID: "Task3",
		ServiceAnnotations: api.Annotations{
			Name: "annotation",
		},
	}
	va := newVolumeAllocator(t)
	va.AllocateTask(t1)
	va.AllocateTask(t2)
	assert.Equal(t, va.IsTaskAllocated(t2), true)
	assert.NotEqual(t, va.IsTaskAllocated(t3), true)
	va.AllocateTask(t3)
	assert.Equal(t, va.IsTaskAllocated(t3), true)
	va.DeallocateTask(t2)
	assert.NotEqual(t, va.IsTaskAllocated(t2), true)
}
