package scheduler

import (
	"testing"

	"github.com/docker/swarmkit/api"
	"github.com/stretchr/testify/assert"
)

func TestRemoveTask(t *testing.T) {
	node := &api.Node{}

	tasks := map[string]*api.Task{
		"task1": {
			ID: "task1",
		},
		"task2": {
			ID: "task2",
		},
	}

	availableResources := api.Resources{
		NanoCPUs:    100000,
		MemoryBytes: 1000000,
	}

	task1 := &api.Task{
		ID: "task1",
	}

	task3 := &api.Task{
		ID: "task3",
	}

	// nodeInfo has no tasks
	nodeInfo := newNodeInfo(node, nil, availableResources)
	assert.False(t, nodeInfo.removeTask(task1))

	// nodeInfo's tasks has taskID
	nodeInfo = newNodeInfo(node, tasks, availableResources)
	assert.True(t, nodeInfo.removeTask(task1))

	// nodeInfo's tasks has no taskID
	assert.False(t, nodeInfo.removeTask(task3))
}

func TestAddTask(t *testing.T) {
	node := &api.Node{}

	tasks := map[string]*api.Task{
		"task1": {
			ID: "task1",
		},
		"task2": {
			ID: "task2",
		},
	}

	availableResources := api.Resources{
		NanoCPUs:    100000,
		MemoryBytes: 1000000,
	}

	task1 := &api.Task{
		ID: "task1",
	}

	task3 := &api.Task{
		ID: "task3",
	}

	nodeInfo := newNodeInfo(node, tasks, availableResources)

	// add task with ID existing
	assert.False(t, nodeInfo.addTask(task1))

	// add task with ID non-existing
	assert.True(t, nodeInfo.addTask(task3))

	// add again
	assert.False(t, nodeInfo.addTask(task3))

}
