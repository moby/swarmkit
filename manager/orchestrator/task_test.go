package orchestrator

import (
	"sort"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/moby/swarmkit/v2/api"
)

// Test IsTaskDirty() for placement constraints.
func TestIsTaskDirty(t *testing.T) {
	service := &api.Service{
		Id:          "id1",
		SpecVersion: &api.Version{Index: 1},
		Spec: &api.ServiceSpec{
			Annotations: &api.Annotations{
				Name: "name1",
			},
			Task: &api.TaskSpec{
				Runtime: &api.TaskSpec_Container{
					Container: &api.ContainerSpec{
						Image: "v:1",
					},
				},
			},
		},
	}

	task := &api.Task{
		Id: "task1",
		Spec: &api.TaskSpec{
			Runtime: &api.TaskSpec_Container{
				Container: &api.ContainerSpec{
					Image: "v:1",
				},
			},
		},
	}

	node := &api.Node{
		Id: "node1",
	}

	assert.False(t, IsTaskDirty(service, task, node))

	// Update only placement constraints.
	service.SpecVersion.Index++
	service.Spec.GetTask().Placement = &api.Placement{}
	service.Spec.GetTask().Placement.Constraints = append(service.Spec.GetTask().GetPlacement().Constraints, "node=node1")
	assert.False(t, IsTaskDirty(service, task, node))

	// Update only placement constraints again.
	service.SpecVersion.Index++
	service.Spec.GetTask().Placement = &api.Placement{}
	service.Spec.GetTask().Placement.Constraints = append(service.Spec.GetTask().GetPlacement().Constraints, "node!=node1")
	assert.True(t, IsTaskDirty(service, task, node))

	// Update only placement constraints
	service.SpecVersion.Index++
	service.Spec.GetTask().Placement = &api.Placement{}
	service.Spec.GetTask().GetContainer().Image = "v:2"
	assert.True(t, IsTaskDirty(service, task, node))
}

func TestIsTaskDirtyPlacementConstraintsOnly(t *testing.T) {
	service := &api.Service{
		Id: "id1",
		Spec: &api.ServiceSpec{
			Annotations: &api.Annotations{
				Name: "name1",
			},
			Task: &api.TaskSpec{
				Runtime: &api.TaskSpec_Container{
					Container: &api.ContainerSpec{
						Image: "v:1",
					},
				},
			},
		},
	}

	task := &api.Task{
		Id: "task1",
		Spec: &api.TaskSpec{
			Runtime: &api.TaskSpec_Container{
				Container: &api.ContainerSpec{
					Image: "v:1",
				},
			},
		},
	}

	assert.False(t, IsTaskDirtyPlacementConstraintsOnly(service.Spec.GetTask(), task))

	// Update only placement constraints.
	service.Spec.GetTask().Placement = &api.Placement{}
	service.Spec.GetTask().Placement.Constraints = append(service.Spec.GetTask().GetPlacement().Constraints, "node==*")
	assert.True(t, IsTaskDirtyPlacementConstraintsOnly(service.Spec.GetTask(), task))

	// Update something else in the task spec.
	service.Spec.GetTask().GetContainer().Image = "v:2"
	assert.False(t, IsTaskDirtyPlacementConstraintsOnly(service.Spec.GetTask(), task))

	// Clear out placement constraints.
	service.Spec.GetTask().Placement.Constraints = nil
	assert.False(t, IsTaskDirtyPlacementConstraintsOnly(service.Spec.GetTask(), task))
}

// Test Task sorting, which is currently based on
// Status.AppliedAt, and then on Status.Timestamp.
func TestTaskSort(t *testing.T) {
	var tasks []*api.Task
	size := 5
	seconds := int64(size)
	for i := range size {
		task := &api.Task{
			Id: "id_" + strconv.Itoa(i),
			Status: &api.TaskStatus{
				Timestamp: &timestamppb.Timestamp{Seconds: seconds},
			},
		}

		seconds--
		tasks = append(tasks, task)
	}

	sort.Sort(TasksByTimestamp(tasks))
	for i, task := range tasks {
		expected := &timestamppb.Timestamp{Seconds: int64(i + 1)}
		// assert.Equal cannot be used on protobuf messages, as it falls back to
		// reflect.DeepEqual, which also walks their internal bookkeeping fields.
		assert.True(t, proto.Equal(expected, task.Status.GetTimestamp()), "expected: %v, actual: %v", expected, task.Status.GetTimestamp())
		assert.Equal(t, "id_"+strconv.Itoa(size-(i+1)), task.Id)
	}

	for i, task := range tasks {
		task.Status.AppliedAt = &timestamppb.Timestamp{Seconds: int64(size - i)}
	}

	sort.Sort(TasksByTimestamp(tasks))
	sort.Sort(TasksByTimestamp(tasks))
	for i, task := range tasks {
		expected := &timestamppb.Timestamp{Seconds: int64(i + 1)}
		assert.True(t, proto.Equal(expected, task.Status.GetAppliedAt()), "expected: %v, actual: %v", expected, task.Status.GetAppliedAt())
		assert.Equal(t, "id_"+strconv.Itoa(i), task.Id)
	}
}
