package scheduler

import (
	"testing"

	"github.com/docker/docker/pkg/stringid"
	"github.com/docker/swarmkit/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConstraintSetTask(t *testing.T) {
	task1 := &api.Task{
		ID: "id1",
		ServiceAnnotations: api.Annotations{
			Name: "name1",
		},

		Spec: api.TaskSpec{
			Runtime: &api.TaskSpec_Container{
				Container: &api.ContainerSpec{
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
	assert.False(t, f.SetTask(task1))

	task1.Spec.Placement = &api.Placement{
		Constraints: []string{"node.name == node-2", "node.labels.operatingsystem != ubuntu"},
	}
	assert.True(t, f.SetTask(task1))
}

func TestConstraintCheck(t *testing.T) {
	task1 := &api.Task{
		ID: "id1",
		ServiceAnnotations: api.Annotations{
			Name: "name1",
		},

		Spec: api.TaskSpec{
			Runtime: &api.TaskSpec_Container{
				Container: &api.ContainerSpec{
					Command: []string{"sh", "-c", "sleep 5"},
					Image:   "alpine",
				},
			},
			Placement: &api.Placement{
				Constraints: []string{"node.name != node-1"},
			},
		},

		Status: api.TaskStatus{
			State: api.TaskStateAssigned,
		},
	}

	fullID := "2mbpbzvuxhkgsiu92z"
	truncatedID := stringid.TruncateID(fullID)

	ni := &NodeInfo{
		Node: &api.Node{
			ID: fullID,
			Spec: api.NodeSpec{
				Annotations: api.Annotations{
					Labels: make(map[string]string),
				},
			},
			Description: &api.NodeDescription{
				Engine: &api.EngineDescription{
					Labels: make(map[string]string),
				},
			},
		},
		Tasks: make(map[string]*api.Task),
	}
	f := ConstraintFilter{}
	require.True(t, f.SetTask(task1))

	// the node without hostname meets node.name noteq constraint
	assert.True(t, f.Check(ni))

	// add hostname to node
	ni.Node.Description.Hostname = "node-1"
	// hostname constraint fails
	assert.False(t, f.Check(ni))

	// set node.name eq constraint to task
	task1.Spec.Placement = &api.Placement{
		Constraints: []string{"node.name == node-1"},
	}
	require.True(t, f.SetTask(task1))
	// the node meets node.name constraint
	assert.True(t, f.Check(ni))

	// test node.id
	task1.Spec.Placement.Constraints = []string{"node.id==" + fullID}
	require.True(t, f.SetTask(task1))
	assert.True(t, f.Check(ni))

	// test truncateID
	task1.Spec.Placement.Constraints = []string{"node.id==" + truncatedID}
	require.True(t, f.SetTask(task1))
	assert.True(t, f.Check(ni))

	// cannot be shorter
	task1.Spec.Placement.Constraints = []string{"node.id==" + truncatedID[0:len(truncatedID)-1]}
	require.True(t, f.SetTask(task1))
	assert.False(t, f.Check(ni))

	// add a label requirement to node
	task1.Spec.Placement = &api.Placement{
		Constraints: []string{"node.name == node-1", "node.labels.operatingsystem != Ubuntu 14.04"},
	}
	require.True(t, f.SetTask(task1))
	// the node meets node.name eq and label noteq constraints
	assert.True(t, f.Check(ni))

	// set node operating system
	ni.Node.Description.Engine.Labels["operatingsystem"] = "Ubuntu 14.04"
	assert.False(t, f.Check(ni))

	// case matters
	ni.Node.Description.Engine.Labels["operatingsystem"] = "ubuntu 14.04"
	assert.True(t, f.Check(ni))

	// extra labels doesn't matter
	ni.Spec.Annotations.Labels["disk"] = "ssd"
	assert.True(t, f.Check(ni))

	// add one more label requirement to task
	task1.Spec.Placement = &api.Placement{
		Constraints: []string{"node.name == node-1",
			"node.labels.operatingsystem != Ubuntu 14.04",
			"node.labels.userpreference == high"},
	}
	require.True(t, f.SetTask(task1))
	assert.False(t, f.Check(ni))

	// add label to Spec.Annotations.Labels
	ni.Spec.Annotations.Labels["userpreference"] = "low"
	assert.False(t, f.Check(ni))
	ni.Spec.Annotations.Labels["userpreference"] = "high"
	assert.True(t, f.Check(ni))
}
