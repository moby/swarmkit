package taskreaper

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/docker/swarmkit/api"
	"github.com/docker/swarmkit/identity"
	"github.com/docker/swarmkit/manager/orchestrator/testutils"
	"github.com/docker/swarmkit/manager/state"
	"github.com/docker/swarmkit/manager/state/store"
	gogotypes "github.com/gogo/protobuf/types"
)

func setupTaskReaperDirty(tr *TaskReaper) {
	tr.dirty[instanceTuple{
		instance:  1,
		serviceID: "id1",
		nodeID:    "node1",
	}] = struct{}{}
	tr.dirty[instanceTuple{
		instance:  1,
		serviceID: "id2",
		nodeID:    "node1",
	}] = struct{}{}
}

// TestTick unit-tests the task reaper tick function.
// 1. Test that the dirty set is cleaned up when the service can't be found.
// 2. Test that the dirty set is cleaned up when the number of total tasks
// is smaller than the retention limit.
// 3. Test that the dirty set and excess tasks in the store are cleaned up
// when there the number of total tasks is greater than the retention limit.
func TestTick(t *testing.T) {
	s := store.NewMemoryStore(nil)
	assert.NotNil(t, s)
	defer s.Close()

	assert.NoError(t, s.Update(func(tx store.Tx) error {
		store.CreateCluster(tx, &api.Cluster{
			ID: identity.NewID(),
			Spec: api.ClusterSpec{
				Annotations: api.Annotations{
					Name: store.DefaultClusterName,
				},
				Orchestration: api.OrchestrationConfig{
					// set TaskHistoryRetentionLimit to a negative value, so
					// that tasks are cleaned up right away.
					TaskHistoryRetentionLimit: 1,
				},
			},
		})
		return nil
	}))

	// create the task reaper.
	taskReaper := New(s)

	// Test # 1
	// Setup the dirty set with entries to
	// verify that the dirty set it cleaned up
	// when the service is not found.
	setupTaskReaperDirty(taskReaper)
	// call tick directly and verify dirty set was cleaned up.
	taskReaper.tick()
	assert.Zero(t, len(taskReaper.dirty))

	// Test # 2
	// Verify that the dirty set it cleaned up
	// when the history limit is set to zero.

	// Create a service in the store for the following test cases.
	service1 := &api.Service{
		ID: "id1",
		Spec: api.ServiceSpec{
			Annotations: api.Annotations{
				Name: "name1",
			},
			Mode: &api.ServiceSpec_Replicated{
				Replicated: &api.ReplicatedService{
					Replicas: 1,
				},
			},
			Task: api.TaskSpec{
				Restart: &api.RestartPolicy{
					// Turn off restart to get an accurate count on tasks.
					Condition: api.RestartOnNone,
					Delay:     gogotypes.DurationProto(0),
				},
			},
		},
	}

	// Create another service in the store for the following test cases.
	service2 := &api.Service{
		ID: "id2",
		Spec: api.ServiceSpec{
			Annotations: api.Annotations{
				Name: "name2",
			},
			Mode: &api.ServiceSpec_Replicated{
				Replicated: &api.ReplicatedService{
					Replicas: 1,
				},
			},
			Task: api.TaskSpec{
				Restart: &api.RestartPolicy{
					// Turn off restart to get an accurate count on tasks.
					Condition: api.RestartOnNone,
					Delay:     gogotypes.DurationProto(0),
				},
			},
		},
	}

	// Create a service.
	err := s.Update(func(tx store.Tx) error {
		assert.NoError(t, store.CreateService(tx, service1))
		assert.NoError(t, store.CreateService(tx, service2))
		return nil
	})
	assert.NoError(t, err)

	// Setup the dirty set with entries to
	// verify that the dirty set it cleaned up
	// when the history limit is set to zero.
	setupTaskReaperDirty(taskReaper)
	taskReaper.taskHistory = 0
	// call tick directly and verify dirty set was cleaned up.
	taskReaper.tick()
	assert.Zero(t, len(taskReaper.dirty))

	// Test # 3
	// Test that the tasks are cleanup when the total number of tasks
	// is greater than the retention limit.

	// Create tasks for both services in the store.
	task1 := &api.Task{
		ID:           "id1task1",
		Slot:         1,
		DesiredState: api.TaskStateShutdown,
		Status: api.TaskStatus{
			State: api.TaskStateShutdown,
		},
		ServiceID: "id1",
		ServiceAnnotations: api.Annotations{
			Name: "name1",
		},
	}

	task2 := &api.Task{
		ID:           "id2task1",
		Slot:         1,
		DesiredState: api.TaskStateShutdown,
		Status: api.TaskStatus{
			State: api.TaskStateShutdown,
		},
		ServiceID: "id2",
		ServiceAnnotations: api.Annotations{
			Name: "name2",
		},
	}

	// Create Tasks.
	err = s.Update(func(tx store.Tx) error {
		assert.NoError(t, store.CreateTask(tx, task1))
		assert.NoError(t, store.CreateTask(tx, task2))
		return nil
	})
	assert.NoError(t, err)

	// Set history to 1 to ensure that the tasks are not cleaned up yet.
	// At the same time, we should be able to test that the dirty set was
	// cleaned up at the end of tick().
	taskReaper.taskHistory = 1
	setupTaskReaperDirty(taskReaper)
	// call tick directly and verify dirty set was cleaned up.
	taskReaper.tick()
	assert.Zero(t, len(taskReaper.dirty))

	// Now test that tick() function cleans up the old tasks from the store.

	// Create new tasks in the store for the same slots to simulate service update.
	task1.Status.State = api.TaskStateNew
	task1.DesiredState = api.TaskStateRunning
	task1.ID = "id1task2"
	task2.Status.State = api.TaskStateNew
	task2.DesiredState = api.TaskStateRunning
	task2.ID = "id2task2"
	err = s.Update(func(tx store.Tx) error {
		assert.NoError(t, store.CreateTask(tx, task1))
		assert.NoError(t, store.CreateTask(tx, task2))
		return nil
	})
	assert.NoError(t, err)

	watch, cancel := state.Watch(s.WatchQueue() /*api.EventCreateTask{}, api.EventUpdateTask{}*/)
	defer cancel()

	// Setup the task reaper dirty set.
	setupTaskReaperDirty(taskReaper)
	// Call tick directly and verify dirty set was cleaned up.
	taskReaper.tick()
	assert.Zero(t, len(taskReaper.dirty))
	// Task reaper should delete the task previously marked for SHUTDOWN.
	deletedTask1 := testutils.WatchTaskDelete(t, watch)
	assert.Equal(t, api.TaskStateShutdown, deletedTask1.Status.State)
	assert.Equal(t, api.TaskStateShutdown, deletedTask1.DesiredState)
	assert.True(t, deletedTask1.ServiceAnnotations.Name == "name1" ||
		deletedTask1.ServiceAnnotations.Name == "name2")

	deletedTask2 := testutils.WatchTaskDelete(t, watch)
	assert.Equal(t, api.TaskStateShutdown, deletedTask2.Status.State)
	assert.Equal(t, api.TaskStateShutdown, deletedTask2.DesiredState)
	assert.True(t, deletedTask1.ServiceAnnotations.Name == "name1" ||
		deletedTask1.ServiceAnnotations.Name == "name2")
}
