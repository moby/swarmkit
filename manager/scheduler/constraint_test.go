package scheduler

import (
	"testing"

	"github.com/docker/swarm-v2/api"
	"github.com/stretchr/testify/assert"
)

func TestEnabled(t *testing.T) {
	task1 := &api.Task{
		ID: "id1",
		ServiceAnnotations: api.Annotations{
			Name: "name1",
		},

		Runtime: &api.Task_Container{
			Container: &api.Container{
				Spec: api.ContainerSpec{
					Command: []string{"sh", "-c", "sleep 5"},
					Image:   "alpine",
				},
			},
		},

		Status: api.TaskStatus{
			State: api.TaskStateAssigned,
		},
	}
	f := ConstraintFilter{}
	enable := f.Enabled(task1)
	assert.False(t, enable)

	ctr := task1.GetContainer()
	ctr.Spec.Placement = &api.Placement{
		Constraints: []string{"node.name == node-2", "node.labels.operatingsystem != ubuntu"},
	}
	enable = f.Enabled(task1)
	assert.True(t, enable)
}

func TestCheck(t *testing.T) {
	task1 := &api.Task{
		ID: "id1",
		ServiceAnnotations: api.Annotations{
			Name: "name1",
		},

		Runtime: &api.Task_Container{
			Container: &api.Container{
				Spec: api.ContainerSpec{
					Command: []string{"sh", "-c", "sleep 5"},
					Image:   "alpine",
					Placement: &api.Placement{
						Constraints: []string{"node.name != node-1"},
					},
				},
			},
		},

		Status: api.TaskStatus{
			State: api.TaskStateAssigned,
		},
	}
	ni := &NodeInfo{
		Node: &api.Node{
			ID: "id1",
			Spec: api.NodeSpec{
				Annotations: api.Annotations{
					Labels: make(map[string]string),
				},
			},
		},
		Tasks: make(map[string]*api.Task),
	}
	f := ConstraintFilter{}

	// the node without hostname meets node.name noteq constraint
	matched := f.Check(task1, ni)
	assert.True(t, matched)

	// add hostname to node
	ni.Node.Description = &api.NodeDescription{
		Hostname: "node-1",
	}
	// hostname constraint fails
	matched = f.Check(task1, ni)
	assert.False(t, matched)

	// set node.name eq constraint to task
	ctr := task1.GetContainer()
	ctr.Spec.Placement = &api.Placement{
		Constraints: []string{"node.name == node-1"},
	}
	// the node meets node.name constraint
	matched = f.Check(task1, ni)
	assert.True(t, matched)

	// add a label requirement to node
	ctr.Spec.Placement = &api.Placement{
		Constraints: []string{"node.name == node-1", "node.labels.operatingsystem != CoreOS 1010.3.0"},
	}
	// the node meets node.name eq and label noteq constraints
	matched = f.Check(task1, ni)
	assert.True(t, matched)

	// set node operating system
	ni.Spec.Annotations.Labels["operatingsystem"] = "CoreOS 1010.3.0"
	matched = f.Check(task1, ni)
	assert.False(t, matched)

	// case matters
	ni.Spec.Annotations.Labels["operatingsystem"] = "coreOS 1010.3.0"
	matched = f.Check(task1, ni)
	assert.True(t, matched)

	// extra labels doesn't matter
	ni.Spec.Annotations.Labels["disk"] = "ssd"
	matched = f.Check(task1, ni)
	assert.True(t, matched)

}
