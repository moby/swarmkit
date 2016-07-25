package equality

import (
	"testing"

	"github.com/docker/swarmkit/api"
	"github.com/stretchr/testify/assert"
)

func TestTasksEqualStable(t *testing.T) {
	const taskCount = 5
	var tasks [taskCount]*api.Task

	for i := 0; i < taskCount; i++ {
		tasks[i] = &api.Task{
			ID:   "task-id",
			Meta: api.Meta{Version: api.Version{Index: 6}},
			Spec: api.TaskSpec{
				Runtime: &api.TaskSpec_Container{
					Container: &api.ContainerSpec{
						Image: "redis:3.0.7",
					},
				},
			},
			ServiceID:    "service-id",
			Slot:         3,
			NodeID:       "node-id",
			Status:       api.TaskStatus{State: api.TaskStateAssigned},
			DesiredState: api.TaskStateReady,
		}
	}

	tasks[1].Status.State = api.TaskStateFailed
	tasks[2].Meta.Version.Index = 7
	tasks[3].DesiredState = api.TaskStateRunning
	tasks[4].Spec.Runtime = &api.TaskSpec_Container{
		Container: &api.ContainerSpec{
			Image: "redis:3.2.1",
		},
	}

	var tests = []struct {
		task        *api.Task
		expected    bool
		failureText string
	}{
		{tasks[1], true, "Tasks with different Status should be equal"},
		{tasks[2], true, "Tasks with different Meta should be equal"},
		{tasks[3], false, "Tasks with different DesiredState are not equal"},
		{tasks[4], false, "Tasks with different Spec are not equal"},
	}
	for _, test := range tests {
		assert.Equal(t, TasksEqualStable(tasks[0], test.task), test.expected, test.failureText)
	}
}
