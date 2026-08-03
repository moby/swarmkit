package scheduler

import (
	"testing"

	"github.com/moby/swarmkit/v2/api"
	"github.com/moby/swarmkit/v2/api/genericresource"
	"github.com/stretchr/testify/assert"
)

func TestRemoveTask(t *testing.T) {
	nodeResourceSpec := &api.Resources{
		NanoCpus:    100000,
		MemoryBytes: 1000000,
		Generic: append(
			genericresource.NewSet("orange", "blue", "red", "green"),
			genericresource.NewDiscrete("apple", 6),
		),
	}

	node := &api.Node{
		Description: &api.NodeDescription{Resources: nodeResourceSpec},
	}

	tasks := map[string]*api.Task{
		"task1": {
			Id: "task1",
		},
		"task2": {
			Id: "task2",
		},
	}

	available := api.Resources{
		NanoCpus:    100000,
		MemoryBytes: 1000000,
		Generic: append(
			genericresource.NewSet("orange", "blue", "red"),
			genericresource.NewDiscrete("apple", 5),
		),
	}

	taskRes := &api.Resources{
		NanoCpus:    5000,
		MemoryBytes: 5000,
		Generic: []*api.GenericResource{
			genericresource.NewDiscrete("apple", 1),
			genericresource.NewDiscrete("orange", 1),
		},
	}

	task1 := &api.Task{
		Id: "task1",
		Spec: &api.TaskSpec{
			Resources: &api.ResourceRequirements{Reservations: taskRes},
		},
		AssignedGenericResources: append(
			genericresource.NewSet("orange", "green"),
			genericresource.NewDiscrete("apple", 1),
		),
	}

	task3 := &api.Task{
		Id: "task3",
	}

	// nodeInfo has no tasks
	nodeInfo := newNodeInfo(node, nil, &available)
	assert.False(t, nodeInfo.removeTask(task1))

	// nodeInfo's tasks has taskID
	nodeInfo = newNodeInfo(node, tasks, &available)
	assert.True(t, nodeInfo.removeTask(task1))

	// nodeInfo's tasks has no taskID
	assert.False(t, nodeInfo.removeTask(task3))

	nodeAvailableResources := nodeInfo.AvailableResources

	cpuLeft := available.NanoCpus + taskRes.NanoCpus
	memoryLeft := available.MemoryBytes + taskRes.MemoryBytes

	assert.Equal(t, cpuLeft, nodeAvailableResources.NanoCpus)
	assert.Equal(t, memoryLeft, nodeAvailableResources.MemoryBytes)

	assert.Equal(t, 4, len(nodeAvailableResources.Generic))

	apples := genericresource.GetResource("apple", nodeAvailableResources.Generic)
	oranges := genericresource.GetResource("orange", nodeAvailableResources.Generic)
	assert.Len(t, apples, 1)
	assert.Len(t, oranges, 3)

	for _, k := range []string{"red", "blue", "green"} {
		assert.True(t, genericresource.HasResource(
			genericresource.NewString("orange", k), oranges),
		)
	}

	assert.Equal(t, int64(6), apples[0].GetDiscreteResourceSpec().GetValue())
}

func TestAddTask(t *testing.T) {
	node := &api.Node{}

	tasks := map[string]*api.Task{
		"task1": {
			Id: "task1",
		},
		"task2": {
			Id: "task2",
		},
	}

	task1 := &api.Task{
		Id: "task1",
	}

	available := api.Resources{
		NanoCpus:    100000,
		MemoryBytes: 1000000,
		Generic: append(
			genericresource.NewSet("orange", "blue", "red"),
			genericresource.NewDiscrete("apple", 5),
		),
	}

	taskRes := &api.Resources{
		NanoCpus:    5000,
		MemoryBytes: 5000,
		Generic: []*api.GenericResource{
			genericresource.NewDiscrete("apple", 2),
			genericresource.NewDiscrete("orange", 1),
		},
	}

	task3 := &api.Task{
		Id: "task3",
		Spec: &api.TaskSpec{
			Resources: &api.ResourceRequirements{Reservations: taskRes},
		},
	}

	nodeInfo := newNodeInfo(node, tasks, &available)

	// add task with ID existing
	assert.False(t, nodeInfo.addTask(task1))

	// add task with ID non-existing
	assert.True(t, nodeInfo.addTask(task3))

	// add again
	assert.False(t, nodeInfo.addTask(task3))

	// Check resource consumption of node
	nodeAvailableResources := nodeInfo.AvailableResources

	cpuLeft := available.NanoCpus - taskRes.NanoCpus
	memoryLeft := available.MemoryBytes - taskRes.MemoryBytes

	assert.Equal(t, cpuLeft, nodeAvailableResources.NanoCpus)
	assert.Equal(t, memoryLeft, nodeAvailableResources.MemoryBytes)

	apples := genericresource.GetResource("apple", nodeAvailableResources.Generic)
	oranges := genericresource.GetResource("orange", nodeAvailableResources.Generic)
	assert.Len(t, apples, 1)
	assert.Len(t, oranges, 1)

	o := oranges[0].GetNamedResourceSpec()
	assert.True(t, o.Value == "blue" || o.Value == "red")
	assert.Equal(t, int64(3), apples[0].GetDiscreteResourceSpec().GetValue())

}
