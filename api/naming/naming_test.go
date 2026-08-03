package naming

import (
	"testing"

	"github.com/moby/swarmkit/v2/api"
	"github.com/stretchr/testify/assert"
)

func TestTaskNaming(t *testing.T) {
	for _, testcase := range []struct {
		Name     string
		Task     *api.Task
		Expected string
	}{
		{
			Name: "Basic",
			Task: &api.Task{
				Id:     "taskID",
				Slot:   10,
				NodeId: "thenodeID",
				ServiceAnnotations: &api.Annotations{
					Name: "theservice",
				},
			},
			Expected: "theservice.10.taskID",
		},
		{
			Name: "Annotations",
			Task: &api.Task{
				Id:     "taskID",
				NodeId: "thenodeID",
				Annotations: &api.Annotations{
					Name: "thisisthetaskname",
				},
				ServiceAnnotations: &api.Annotations{
					Name: "theservice",
				},
			},
			Expected: "thisisthetaskname",
		},
		{
			Name: "NoSlot",
			Task: &api.Task{
				Id:     "taskID",
				NodeId: "thenodeID",
				ServiceAnnotations: &api.Annotations{
					Name: "theservice",
				},
			},
			Expected: "theservice.thenodeID.taskID",
		},
	} {
		t.Run(testcase.Name, func(t *testing.T) {
			t.Parallel()
			name := Task(testcase.Task)
			assert.Equal(t, name, testcase.Expected)
		})
	}
}
