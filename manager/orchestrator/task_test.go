package orchestrator

import (
	"testing"

	"github.com/docker/swarmkit/api"
	"github.com/stretchr/testify/assert"
)

func TestIsTaskDirty(t *testing.T) {
	s := &api.Service{
		Spec: api.ServiceSpec{
			Task: api.TaskSpec{
				Networks: []*api.NetworkAttachmentConfig{
					{
						Target: "network1",
					},
					{
						Target: "network2",
					},
					{
						Target: "network3",
					},
				},
			},
		},
		SpecVersion: &api.Version{Index: 5},
	}

	// Task with spec version

	t1 := &api.Task{
		Spec:        *s.Spec.Task.Copy(),
		SpecVersion: s.SpecVersion.Copy(),
	}

	assert.False(t, IsTaskDirty(s, t1))

	// Task without spec version

	t2 := &api.Task{
		Spec: *s.Spec.Task.Copy(),
	}

	assert.False(t, IsTaskDirty(s, t2))

	// Task with different spec version

	t3 := &api.Task{
		Spec:        *s.Spec.Task.Copy(),
		SpecVersion: &api.Version{Index: 6},
	}

	assert.False(t, IsTaskDirty(s, t3))

	// Task with networks in different order
	t4 := &api.Task{
		Spec:        *s.Spec.Task.Copy(),
		SpecVersion: &api.Version{Index: 6},
	}

	t4.Spec.Networks[0] = s.Spec.Task.Networks[0]
	t4.Spec.Networks[1] = s.Spec.Task.Networks[2]
	t4.Spec.Networks[2] = s.Spec.Task.Networks[1]

	assert.False(t, IsTaskDirty(s, t4))

	// Task with different networks
	t5 := &api.Task{
		Spec:        *s.Spec.Task.Copy(),
		SpecVersion: &api.Version{Index: 6},
	}

	t5.Spec.Networks = s.Spec.Task.Networks[:1]

	assert.True(t, IsTaskDirty(s, t5))
}
