package taskreaper

import (
	"context"

	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/swarmkit/api"
	"github.com/docker/swarmkit/manager/state/store"
)

// TestTaskReaperInit tests that the task reaper correctly cleans up tasks when
// it is initialized. This will happen every time cluster leadership changes.
func TestTaskReaperInit(t *testing.T) {
	// start up the memory store
	ctx := context.Background()
	s := store.NewMemoryStore(nil)
	require.NotNil(t, s)
	defer s.Close()

	// Create the basic cluster with precooked tasks we need for the taskreaper
	cluster := &api.Cluster{
		Spec: api.ClusterSpec{
			Annotations: api.Annotations{
				Name: store.DefaultClusterName,
			},
			Orchestration: api.OrchestrationConfig{
				TaskHistoryRetentionLimit: 2,
			},
		},
	}

	// this service is alive and active, has no tasks to clean up
	service := &api.Service{
		ID: "cleanservice",
		Spec: api.ServiceSpec{
			Annotations: api.Annotations{
				Name: "cleanservice",
			},
			Task: api.TaskSpec{
				// the runtime spec isn't looked at and doesn't really need to
				// be filled in
				Runtime: &api.TaskSpec_Container{
					Container: &api.ContainerSpec{},
				},
			},
			Mode: &api.ServiceSpec_Replicated{
				Replicated: &api.ReplicatedService{
					Replicas: 2,
				},
			},
		},
	}

	// Two clean tasks, these should not be removed
	cleantask1 := &api.Task{
		ID:           "cleantask1",
		Slot:         1,
		DesiredState: api.TaskStateRunning,
		Status: api.TaskStatus{
			State: api.TaskStateRunning,
		},
		ServiceID: "cleanservice",
	}

	cleantask2 := &api.Task{
		ID:           "cleantask2",
		Slot:         2,
		DesiredState: api.TaskStateRunning,
		Status: api.TaskStatus{
			State: api.TaskStateRunning,
		},
		ServiceID: "cleanservice",
	}

	// this is an old task from when an earlier task failed. It should not be
	// removed because it's retained history
	retainedtask := &api.Task{
		ID:           "retainedtask",
		Slot:         1,
		DesiredState: api.TaskStateShutdown,
		Status: api.TaskStatus{
			State: api.TaskStateFailed,
		},
		ServiceID: "cleanservice",
	}

	// This is a removed task after cleanservice was scaled down
	removedtask := &api.Task{
		ID:           "removedtask",
		Slot:         3,
		DesiredState: api.TaskStateRemove,
		Status: api.TaskStatus{
			State: api.TaskStateShutdown,
		},
		ServiceID: "cleanservice",
	}

	// two tasks belonging to a service that does not exist.
	// this first one is sitll running and should not be cleaned up
	terminaltask1 := &api.Task{
		ID:           "terminaltask1",
		Slot:         1,
		DesiredState: api.TaskStateRemove,
		Status: api.TaskStatus{
			State: api.TaskStateRunning,
		},
		ServiceID: "goneservice",
	}

	// this second task is shutdown, and can be cleaned up
	terminaltask2 := &api.Task{
		ID:           "terminaltask2",
		Slot:         2,
		DesiredState: api.TaskStateRemove,
		Status: api.TaskStatus{
			// use COMPLETE because it's the earliest terminal state
			State: api.TaskStateCompleted,
		},
		ServiceID: "goneservice",
	}

	// this service is marked for removal and has zero tasks in the store
	serviceMarkedForRemovalNoTasks := &api.Service{
		ID: "serviceMarkedForRemovalNoTasks",
		Spec: api.ServiceSpec{
			Annotations: api.Annotations{
				Name: "serviceMarkedForRemovalNoTasks",
			},
			Task: api.TaskSpec{
				// the runtime spec isn't looked at and doesn't really need to
				// be filled in
				Runtime: &api.TaskSpec_Container{
					Container: &api.ContainerSpec{},
				},
			},
			Mode: &api.ServiceSpec_Replicated{
				Replicated: &api.ReplicatedService{
					Replicas: 0,
				},
			},
		},
		MarkedForRemoval: true,
	}

	// this service is marked for removal and has non-zero tasks in the store
	serviceMarkedForRemoval := &api.Service{
		ID: "serviceMarkedForRemoval",
		Spec: api.ServiceSpec{
			Annotations: api.Annotations{
				Name: "serviceMarkedForRemoval",
			},
			Task: api.TaskSpec{
				// the runtime spec isn't looked at and doesn't really need to
				// be filled in
				Runtime: &api.TaskSpec_Container{
					Container: &api.ContainerSpec{},
				},
			},
			Mode: &api.ServiceSpec_Replicated{
				Replicated: &api.ReplicatedService{
					Replicas: 2,
				},
			},
		},
		MarkedForRemoval: true,
	}

	// This is a removed task after its service was marked for removal
	removedTaskForRemovedService := &api.Task{
		ID:           "removedTaskForRemovedService",
		Slot:         1,
		DesiredState: api.TaskStateRemove,
		Status: api.TaskStatus{
			State: api.TaskStateShutdown,
		},
		ServiceID: "serviceMarkedForRemoval",
	}

	err := s.Update(func(tx store.Tx) error {
		require.NoError(t, store.CreateCluster(tx, cluster))
		require.NoError(t, store.CreateService(tx, service))
		require.NoError(t, store.CreateTask(tx, cleantask1))
		require.NoError(t, store.CreateTask(tx, cleantask2))
		require.NoError(t, store.CreateTask(tx, retainedtask))
		require.NoError(t, store.CreateTask(tx, removedtask))
		require.NoError(t, store.CreateTask(tx, terminaltask1))
		require.NoError(t, store.CreateTask(tx, terminaltask2))
		require.NoError(t, store.CreateService(tx, serviceMarkedForRemovalNoTasks))
		require.NoError(t, store.CreateService(tx, serviceMarkedForRemoval))
		require.NoError(t, store.CreateTask(tx, removedTaskForRemovedService))
		return nil
	})
	require.NoError(t, err, "Error setting up test fixtures")

	// set up the task reaper we'll use for this test
	reaper := New(s)

	// Now, start the reaper
	go reaper.Run(ctx)

	// And then stop the reaper. This will cause the reaper to run through its
	// whole init phase and then immediately enter the loop body, get the stop
	// signal, and exit. plus, it will block until that loop body has been
	// reached and the reaper is stopped.
	reaper.Stop()

	// Now check that all of the tasks are in the state we expect
	s.View(func(tx store.ReadTx) {
		// the first two clean tasks should exist
		assert.NotNil(t, store.GetTask(tx, "cleantask1"))
		assert.NotNil(t, store.GetTask(tx, "cleantask1"))
		// the retained task should still exist
		assert.NotNil(t, store.GetTask(tx, "retainedtask"))
		// the removed task should be gone
		assert.Nil(t, store.GetTask(tx, "removedtask"))
		// the first terminal task, which has not yet shut down, should exist
		assert.NotNil(t, store.GetTask(tx, "terminaltask1"))
		// and the second terminal task should have been removed
		assert.Nil(t, store.GetTask(tx, "terminaltask2"))
		// the service with one task that was marked for removal should be gone
		assert.Nil(t, store.GetService(tx, "serviceMarkedForRemoval"))
		// and its task also
		assert.Nil(t, store.GetTask(tx, "removedTaskForRemovedService"))
		// the service with no tasks that was marked for removal should be gone
		assert.Nil(t, store.GetService(tx, "serviceMarkedForRemovalNoTasks"))
	})
}
