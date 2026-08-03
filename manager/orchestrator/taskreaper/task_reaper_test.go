package taskreaper

import (
	"context"
	"fmt"
	"time"

	"github.com/moby/swarmkit/v2/manager/orchestrator"

	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/moby/swarmkit/v2/api"
	"github.com/moby/swarmkit/v2/identity"
	"github.com/moby/swarmkit/v2/manager/orchestrator/replicated"
	"github.com/moby/swarmkit/v2/manager/orchestrator/testutils"
	"github.com/moby/swarmkit/v2/manager/state"
	"github.com/moby/swarmkit/v2/manager/state/store"
	durationpb "google.golang.org/protobuf/types/known/durationpb"
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
		Spec: &api.ClusterSpec{
			Annotations: &api.Annotations{
				Name: store.DefaultClusterName,
			},
			Orchestration: &api.OrchestrationConfig{
				TaskHistoryRetentionLimit: 2,
			},
		},
	}

	// this service is alive and active, has no tasks to clean up
	service := &api.Service{
		Id: "cleanservice",
		Spec: &api.ServiceSpec{
			Annotations: &api.Annotations{
				Name: "cleanservice",
			},
			Task: &api.TaskSpec{
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
		Id:           "cleantask1",
		Slot:         1,
		DesiredState: api.TaskState_RUNNING,
		Status: &api.TaskStatus{
			State: api.TaskState_RUNNING,
		},
		ServiceId: "cleanservice",
	}

	cleantask2 := &api.Task{
		Id:           "cleantask2",
		Slot:         2,
		DesiredState: api.TaskState_RUNNING,
		Status: &api.TaskStatus{
			State: api.TaskState_RUNNING,
		},
		ServiceId: "cleanservice",
	}

	// this is an old task from when an earlier task failed. It should not be
	// removed because it's retained history
	retainedtask := &api.Task{
		Id:           "retainedtask",
		Slot:         1,
		DesiredState: api.TaskState_SHUTDOWN,
		Status: &api.TaskStatus{
			State: api.TaskState_FAILED,
		},
		ServiceId: "cleanservice",
	}

	// This is a removed task after cleanservice was scaled down
	removedtask := &api.Task{
		Id:           "removedtask",
		Slot:         3,
		DesiredState: api.TaskState_REMOVE,
		Status: &api.TaskStatus{
			State: api.TaskState_SHUTDOWN,
		},
		ServiceId: "cleanservice",
	}

	// some tasks belonging to a service that does not exist.
	// this first one is sitll running and should not be cleaned up
	terminaltask1 := &api.Task{
		Id:           "terminaltask1",
		Slot:         1,
		DesiredState: api.TaskState_REMOVE,
		Status: &api.TaskStatus{
			State: api.TaskState_RUNNING,
		},
		ServiceId: "goneservice",
	}

	// this second task is shutdown, and can be cleaned up
	terminaltask2 := &api.Task{
		Id:           "terminaltask2",
		Slot:         2,
		DesiredState: api.TaskState_REMOVE,
		Status: &api.TaskStatus{
			// use COMPLETE because it's the earliest terminal state
			State: api.TaskState_COMPLETE,
		},
		ServiceId: "goneservice",
	}

	// this third task was never assigned, and should be removed
	earlytask1 := &api.Task{
		Id:           "earlytask1",
		Slot:         3,
		DesiredState: api.TaskState_REMOVE,
		Status: &api.TaskStatus{
			State: api.TaskState_PENDING,
		},
		ServiceId: "goneservice",
	}

	// this fourth task was never assigned, and should be removed
	earlytask2 := &api.Task{
		Id:           "earlytask2",
		Slot:         4,
		DesiredState: api.TaskState_REMOVE,
		Status: &api.TaskStatus{
			State: api.TaskState_NEW,
		},
		ServiceId: "goneservice",
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
		require.NoError(t, store.CreateTask(tx, earlytask1))
		require.NoError(t, store.CreateTask(tx, earlytask2))
		return nil
	})
	require.NoError(t, err, "Error setting up test fixtures")

	// set up the task reaper we'll use for this test
	reaper := New(s)

	// Now, start the reaper
	testutils.EnsureRuns(func() { reaper.Run(ctx) })

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
		// the second terminal task should have been removed
		assert.Nil(t, store.GetTask(tx, "terminaltask2"))
		// the first early task, which was never assigned, should be removed
		assert.Nil(t, store.GetTask(tx, "earlytask1"))
		// the second early task, which was never assigned, should be removed
		assert.Nil(t, store.GetTask(tx, "earlytask2"))
	})
}

func TestTaskHistory(t *testing.T) {
	ctx := context.Background()
	s := store.NewMemoryStore(nil)
	assert.NotNil(t, s)
	defer s.Close()

	assert.NoError(t, s.Update(func(tx store.Tx) error {
		store.CreateCluster(tx, &api.Cluster{
			Id: identity.NewID(),
			Spec: &api.ClusterSpec{
				Annotations: &api.Annotations{
					Name: store.DefaultClusterName,
				},
				Orchestration: &api.OrchestrationConfig{
					TaskHistoryRetentionLimit: 2,
				},
			},
		})
		return nil
	}))

	taskReaper := New(s)
	defer taskReaper.Stop()
	orchestrator := replicated.NewReplicatedOrchestrator(s)
	defer orchestrator.Stop()

	watch, cancel := state.Watch(s.WatchQueue() /*api.EventCreateTask{}, api.EventUpdateTask{}*/)
	defer cancel()

	// Create a service with two instances specified before the orchestrator is
	// started. This should result in two tasks when the orchestrator
	// starts up.
	err := s.Update(func(tx store.Tx) error {
		j1 := &api.Service{
			Id: "id1",
			Spec: &api.ServiceSpec{
				Annotations: &api.Annotations{
					Name: "name1",
				},
				Mode: &api.ServiceSpec_Replicated{
					Replicated: &api.ReplicatedService{
						Replicas: 2,
					},
				},
				Task: &api.TaskSpec{
					Restart: &api.RestartPolicy{
						Condition: api.RestartPolicy_ANY,
						Delay:     durationpb.New(0),
					},
				},
			},
		}
		assert.NoError(t, store.CreateService(tx, j1))
		return nil
	})
	assert.NoError(t, err)

	// Start the orchestrator.
	testutils.EnsureRuns(func() {
		assert.NoError(t, orchestrator.Run(ctx))
	})
	testutils.EnsureRuns(func() { taskReaper.Run(ctx) })

	observedTask1 := testutils.WatchTaskCreate(t, watch)
	assert.Equal(t, observedTask1.Status.GetState(), api.TaskState_NEW)
	assert.Equal(t, observedTask1.GetServiceAnnotations().GetName(), "name1")

	observedTask2 := testutils.WatchTaskCreate(t, watch)
	assert.Equal(t, observedTask2.Status.GetState(), api.TaskState_NEW)
	assert.Equal(t, observedTask2.GetServiceAnnotations().GetName(), "name1")

	// Fail both tasks. They should both get restarted.
	updatedTask1 := observedTask1.Copy()
	updatedTask1.Status.State = api.TaskState_FAILED
	updatedTask1.ServiceAnnotations = &api.Annotations{Name: "original"}
	updatedTask2 := observedTask2.Copy()
	updatedTask2.Status.State = api.TaskState_FAILED
	updatedTask2.ServiceAnnotations = &api.Annotations{Name: "original"}
	err = s.Update(func(tx store.Tx) error {
		assert.NoError(t, store.UpdateTask(tx, updatedTask1))
		assert.NoError(t, store.UpdateTask(tx, updatedTask2))
		return nil
	})

	testutils.Expect(t, watch, state.EventCommit{})
	testutils.Expect(t, watch, api.EventUpdateTask{})
	testutils.Expect(t, watch, api.EventUpdateTask{})
	testutils.Expect(t, watch, state.EventCommit{})

	testutils.Expect(t, watch, api.EventUpdateTask{})
	observedTask3 := testutils.WatchTaskCreate(t, watch)
	assert.Equal(t, observedTask3.Status.GetState(), api.TaskState_NEW)
	assert.Equal(t, observedTask3.GetServiceAnnotations().GetName(), "name1")

	testutils.Expect(t, watch, api.EventUpdateTask{})
	observedTask4 := testutils.WatchTaskCreate(t, watch)
	assert.Equal(t, observedTask4.Status.GetState(), api.TaskState_NEW)
	assert.Equal(t, observedTask4.GetServiceAnnotations().GetName(), "name1")

	// Fail these replacement tasks. Since TaskHistory is set to 2, this
	// should cause the oldest tasks for each instance to get deleted.
	updatedTask3 := observedTask3.Copy()
	updatedTask3.Status.State = api.TaskState_FAILED
	updatedTask4 := observedTask4.Copy()
	updatedTask4.Status.State = api.TaskState_FAILED
	err = s.Update(func(tx store.Tx) error {
		assert.NoError(t, store.UpdateTask(tx, updatedTask3))
		assert.NoError(t, store.UpdateTask(tx, updatedTask4))
		return nil
	})

	deletedTask1 := testutils.WatchTaskDelete(t, watch)
	deletedTask2 := testutils.WatchTaskDelete(t, watch)

	assert.Equal(t, api.TaskState_FAILED, deletedTask1.Status.GetState())
	assert.Equal(t, "original", deletedTask1.GetServiceAnnotations().GetName())
	assert.Equal(t, api.TaskState_FAILED, deletedTask2.Status.GetState())
	assert.Equal(t, "original", deletedTask2.GetServiceAnnotations().GetName())

	var foundTasks []*api.Task
	s.View(func(tx store.ReadTx) {
		foundTasks, err = store.FindTasks(tx, store.All)
	})
	assert.NoError(t, err)
	assert.Len(t, foundTasks, 4)
}

// TestTaskStateRemoveOnScaledown tests that on service scale down, task desired
// states are set to REMOVE. Then, when the agent shuts the task down (simulated
// by setting the task state to SHUTDOWN), the task reaper actually deletes
// the tasks from the store.
func TestTaskStateRemoveOnScaledown(t *testing.T) {
	ctx := context.Background()
	s := store.NewMemoryStore(nil)
	assert.NotNil(t, s)
	defer s.Close()

	assert.NoError(t, s.Update(func(tx store.Tx) error {
		store.CreateCluster(tx, &api.Cluster{
			Id: identity.NewID(),
			Spec: &api.ClusterSpec{
				Annotations: &api.Annotations{
					Name: store.DefaultClusterName,
				},
				Orchestration: &api.OrchestrationConfig{
					// set TaskHistoryRetentionLimit to a negative value, so
					// that it is not considered in this test
					TaskHistoryRetentionLimit: -1,
				},
			},
		})
		return nil
	}))

	taskReaper := New(s)
	defer taskReaper.Stop()
	orchestrator := replicated.NewReplicatedOrchestrator(s)
	defer orchestrator.Stop()

	// watch all incoming events
	watch, cancel := state.Watch(s.WatchQueue())
	defer cancel()

	service1 := &api.Service{
		Id: "id1",
		Spec: &api.ServiceSpec{
			Annotations: &api.Annotations{
				Name: "name1",
			},
			Mode: &api.ServiceSpec_Replicated{
				Replicated: &api.ReplicatedService{
					Replicas: 2,
				},
			},
			Task: &api.TaskSpec{
				Restart: &api.RestartPolicy{
					Condition: api.RestartPolicy_ANY,
					Delay:     durationpb.New(0),
				},
			},
		},
	}

	// Create a service with two instances specified before the orchestrator is
	// started. This should result in two tasks when the orchestrator
	// starts up.
	err := s.Update(func(tx store.Tx) error {
		assert.NoError(t, store.CreateService(tx, service1))
		return nil
	})
	assert.NoError(t, err)

	// Start the orchestrator.
	testutils.EnsureRuns(func() { assert.NoError(t, orchestrator.Run(ctx)) })
	testutils.EnsureRuns(func() { taskReaper.Run(ctx) })

	observedTask1 := testutils.WatchTaskCreate(t, watch)
	assert.Equal(t, observedTask1.Status.GetState(), api.TaskState_NEW)
	assert.Equal(t, observedTask1.GetServiceAnnotations().GetName(), "name1")

	observedTask2 := testutils.WatchTaskCreate(t, watch)
	assert.Equal(t, observedTask2.Status.GetState(), api.TaskState_NEW)
	assert.Equal(t, observedTask2.GetServiceAnnotations().GetName(), "name1")

	// Set both tasks to RUNNING, so the service is successfully running
	updatedTask1 := observedTask1.Copy()
	updatedTask1.Status.State = api.TaskState_RUNNING
	updatedTask1.ServiceAnnotations = &api.Annotations{Name: "original"}
	updatedTask2 := observedTask2.Copy()
	updatedTask2.Status.State = api.TaskState_RUNNING
	updatedTask2.ServiceAnnotations = &api.Annotations{Name: "original"}
	err = s.Update(func(tx store.Tx) error {
		assert.NoError(t, store.UpdateTask(tx, updatedTask1))
		assert.NoError(t, store.UpdateTask(tx, updatedTask2))
		return nil
	})

	testutils.Expect(t, watch, state.EventCommit{})
	testutils.Expect(t, watch, api.EventUpdateTask{})
	testutils.Expect(t, watch, api.EventUpdateTask{})
	testutils.Expect(t, watch, state.EventCommit{})

	// Scale the service down to one instance. This should trigger one of the task
	// statuses to be set to REMOVE.
	service1.Spec.GetReplicated().Replicas = 1
	err = s.Update(func(tx store.Tx) error {
		assert.NoError(t, store.UpdateService(tx, service1))
		return nil
	})

	observedTask3 := testutils.WatchTaskUpdate(t, watch)
	assert.Equal(t, observedTask3.DesiredState, api.TaskState_REMOVE)
	assert.Equal(t, observedTask3.GetServiceAnnotations().GetName(), "original")

	testutils.Expect(t, watch, state.EventCommit{})

	// Now the task for which desired state was set to REMOVE must be deleted by the task reaper.
	// Shut this task down first (simulates shut down by agent)
	updatedTask3 := observedTask3.Copy()
	updatedTask3.Status.State = api.TaskState_SHUTDOWN
	err = s.Update(func(tx store.Tx) error {
		assert.NoError(t, store.UpdateTask(tx, updatedTask3))
		return nil
	})

	deletedTask1 := testutils.WatchTaskDelete(t, watch)

	assert.Equal(t, api.TaskState_SHUTDOWN, deletedTask1.Status.GetState())
	assert.Equal(t, "original", deletedTask1.GetServiceAnnotations().GetName())

	var foundTasks []*api.Task
	s.View(func(tx store.ReadTx) {
		foundTasks, err = store.FindTasks(tx, store.All)
	})
	assert.NoError(t, err)
	assert.Len(t, foundTasks, 1)
}

// TestTaskStateRemoveOnServiceRemoval tests that on service removal, task desired
// states are set to REMOVE. Then, when the agent shuts the task down (simulated
// by setting the task state to SHUTDOWN), the task reaper actually deletes
// the tasks from the store.
func TestTaskStateRemoveOnServiceRemoval(t *testing.T) {
	ctx := context.Background()
	s := store.NewMemoryStore(nil)
	assert.NotNil(t, s)
	defer s.Close()

	assert.NoError(t, s.Update(func(tx store.Tx) error {
		store.CreateCluster(tx, &api.Cluster{
			Id: identity.NewID(),
			Spec: &api.ClusterSpec{
				Annotations: &api.Annotations{
					Name: store.DefaultClusterName,
				},
				Orchestration: &api.OrchestrationConfig{
					// set TaskHistoryRetentionLimit to a negative value, so
					// that it is not considered in this test
					TaskHistoryRetentionLimit: -1,
				},
			},
		})
		return nil
	}))

	taskReaper := New(s)
	defer taskReaper.Stop()
	orchestrator := replicated.NewReplicatedOrchestrator(s)
	defer orchestrator.Stop()

	watch, cancel := state.Watch(s.WatchQueue() /*api.EventCreateTask{}, api.EventUpdateTask{}*/)
	defer cancel()

	service1 := &api.Service{
		Id: "id1",
		Spec: &api.ServiceSpec{
			Annotations: &api.Annotations{
				Name: "name1",
			},
			Mode: &api.ServiceSpec_Replicated{
				Replicated: &api.ReplicatedService{
					Replicas: 2,
				},
			},
			Task: &api.TaskSpec{
				Restart: &api.RestartPolicy{
					Condition: api.RestartPolicy_ANY,
					Delay:     durationpb.New(0),
				},
			},
		},
	}

	// Create a service with two instances specified before the orchestrator is
	// started. This should result in two tasks when the orchestrator
	// starts up.
	err := s.Update(func(tx store.Tx) error {
		assert.NoError(t, store.CreateService(tx, service1))
		return nil
	})
	assert.NoError(t, err)

	// Start the orchestrator.
	testutils.EnsureRuns(func() {
		assert.NoError(t, orchestrator.Run(ctx))
	})
	testutils.EnsureRuns(func() { taskReaper.Run(ctx) })

	observedTask1 := testutils.WatchTaskCreate(t, watch)
	assert.Equal(t, observedTask1.Status.GetState(), api.TaskState_NEW)
	assert.Equal(t, observedTask1.GetServiceAnnotations().GetName(), "name1")

	observedTask2 := testutils.WatchTaskCreate(t, watch)
	assert.Equal(t, observedTask2.Status.GetState(), api.TaskState_NEW)
	assert.Equal(t, observedTask2.GetServiceAnnotations().GetName(), "name1")

	// Set both tasks to RUNNING, so the service is successfully running
	updatedTask1 := observedTask1.Copy()
	updatedTask1.Status.State = api.TaskState_RUNNING
	updatedTask1.ServiceAnnotations = &api.Annotations{Name: "original"}
	updatedTask2 := observedTask2.Copy()
	updatedTask2.Status.State = api.TaskState_RUNNING
	updatedTask2.ServiceAnnotations = &api.Annotations{Name: "original"}
	err = s.Update(func(tx store.Tx) error {
		assert.NoError(t, store.UpdateTask(tx, updatedTask1))
		assert.NoError(t, store.UpdateTask(tx, updatedTask2))
		return nil
	})

	testutils.Expect(t, watch, state.EventCommit{})
	testutils.Expect(t, watch, api.EventUpdateTask{})
	testutils.Expect(t, watch, api.EventUpdateTask{})
	testutils.Expect(t, watch, state.EventCommit{})

	// Delete the service. This should trigger both the task desired statuses to be set to REMOVE.
	err = s.Update(func(tx store.Tx) error {
		assert.NoError(t, store.DeleteService(tx, service1.Id))
		return nil
	})

	observedTask3 := testutils.WatchTaskUpdate(t, watch)
	assert.Equal(t, observedTask3.DesiredState, api.TaskState_REMOVE)
	assert.Equal(t, observedTask3.GetServiceAnnotations().GetName(), "original")
	observedTask4 := testutils.WatchTaskUpdate(t, watch)
	assert.Equal(t, observedTask4.DesiredState, api.TaskState_REMOVE)
	assert.Equal(t, observedTask4.GetServiceAnnotations().GetName(), "original")

	testutils.Expect(t, watch, state.EventCommit{})

	// Now the tasks must be deleted by the task reaper.
	// Shut them down first (simulates shut down by agent)
	updatedTask3 := observedTask3.Copy()
	updatedTask3.Status.State = api.TaskState_SHUTDOWN
	updatedTask4 := observedTask4.Copy()
	updatedTask4.Status.State = api.TaskState_SHUTDOWN
	err = s.Update(func(tx store.Tx) error {
		assert.NoError(t, store.UpdateTask(tx, updatedTask3))
		assert.NoError(t, store.UpdateTask(tx, updatedTask4))
		return nil
	})

	deletedTask1 := testutils.WatchTaskDelete(t, watch)
	assert.Equal(t, api.TaskState_SHUTDOWN, deletedTask1.Status.GetState())
	assert.Equal(t, "original", deletedTask1.GetServiceAnnotations().GetName())

	deletedTask2 := testutils.WatchTaskDelete(t, watch)
	assert.Equal(t, api.TaskState_SHUTDOWN, deletedTask2.Status.GetState())
	assert.Equal(t, "original", deletedTask1.GetServiceAnnotations().GetName())

	var foundTasks []*api.Task
	s.View(func(tx store.ReadTx) {
		foundTasks, err = store.FindTasks(tx, store.All)
	})
	assert.NoError(t, err)
	assert.Len(t, foundTasks, 0)
}

// TestServiceRemoveDeadTasks tests removal of dead tasks
// (old shutdown tasks) on service remove.
func TestServiceRemoveDeadTasks(t *testing.T) {
	ctx := context.Background()
	s := store.NewMemoryStore(nil)
	assert.NotNil(t, s)
	defer s.Close()

	assert.NoError(t, s.Update(func(tx store.Tx) error {
		store.CreateCluster(tx, &api.Cluster{
			Id: identity.NewID(),
			Spec: &api.ClusterSpec{
				Annotations: &api.Annotations{
					Name: store.DefaultClusterName,
				},
				Orchestration: &api.OrchestrationConfig{
					// set TaskHistoryRetentionLimit to a negative value, so
					// that it is not considered in this test
					TaskHistoryRetentionLimit: -1,
				},
			},
		})
		return nil
	}))

	taskReaper := New(s)
	defer taskReaper.Stop()
	orchestrator := replicated.NewReplicatedOrchestrator(s)
	defer orchestrator.Stop()

	watch, cancel := state.Watch(s.WatchQueue() /*api.EventCreateTask{}, api.EventUpdateTask{}*/)
	defer cancel()

	service1 := &api.Service{
		Id: "id1",
		Spec: &api.ServiceSpec{
			Annotations: &api.Annotations{
				Name: "name1",
			},
			Mode: &api.ServiceSpec_Replicated{
				Replicated: &api.ReplicatedService{
					Replicas: 2,
				},
			},
			Task: &api.TaskSpec{
				Restart: &api.RestartPolicy{
					// Turn off restart to get an accurate count on tasks.
					Condition: api.RestartPolicy_NONE,
					Delay:     durationpb.New(0),
				},
			},
		},
	}

	// Create a service with two instances specified before the orchestrator is
	// started. This should result in two tasks when the orchestrator
	// starts up.
	err := s.Update(func(tx store.Tx) error {
		assert.NoError(t, store.CreateService(tx, service1))
		return nil
	})
	assert.NoError(t, err)

	// Start the orchestrator and the reaper.
	testutils.EnsureRuns(func() {
		assert.NoError(t, orchestrator.Run(ctx))
	})
	testutils.EnsureRuns(func() { taskReaper.Run(ctx) })

	observedTask1 := testutils.WatchTaskCreate(t, watch)
	assert.Equal(t, api.TaskState_NEW, observedTask1.Status.GetState())
	assert.Equal(t, observedTask1.GetServiceAnnotations().GetName(), "name1")

	observedTask2 := testutils.WatchTaskCreate(t, watch)
	assert.Equal(t, api.TaskState_NEW, observedTask2.Status.GetState())
	assert.Equal(t, observedTask2.GetServiceAnnotations().GetName(), "name1")

	// Set both task states to RUNNING.
	updatedTask1 := observedTask1.Copy()
	updatedTask1.Status.State = api.TaskState_RUNNING
	updatedTask1.ServiceAnnotations = &api.Annotations{Name: "original"}
	updatedTask2 := observedTask2.Copy()
	updatedTask2.Status.State = api.TaskState_RUNNING
	updatedTask2.ServiceAnnotations = &api.Annotations{Name: "original"}
	err = s.Update(func(tx store.Tx) error {
		assert.NoError(t, store.UpdateTask(tx, updatedTask1))
		assert.NoError(t, store.UpdateTask(tx, updatedTask2))
		return nil
	})
	require.NoError(t, err)

	testutils.Expect(t, watch, state.EventCommit{})
	testutils.Expect(t, watch, api.EventUpdateTask{})
	testutils.Expect(t, watch, api.EventUpdateTask{})
	testutils.Expect(t, watch, state.EventCommit{})

	// Set both tasks to COMPLETED.
	updatedTask3 := observedTask1.Copy()
	updatedTask3.DesiredState = api.TaskState_COMPLETE
	updatedTask3.Status.State = api.TaskState_COMPLETE
	updatedTask3.ServiceAnnotations = &api.Annotations{Name: "original"}
	updatedTask4 := observedTask2.Copy()
	updatedTask4.DesiredState = api.TaskState_COMPLETE
	updatedTask4.Status.State = api.TaskState_COMPLETE
	updatedTask4.ServiceAnnotations = &api.Annotations{Name: "original"}
	err = s.Update(func(tx store.Tx) error {
		assert.NoError(t, store.UpdateTask(tx, updatedTask3))
		assert.NoError(t, store.UpdateTask(tx, updatedTask4))
		return nil
	})
	require.NoError(t, err)

	// Verify state is set to COMPLETED
	observedTask3 := testutils.WatchTaskUpdate(t, watch)
	assert.Equal(t, api.TaskState_COMPLETE, observedTask3.Status.GetState())
	assert.Equal(t, "original", observedTask3.GetServiceAnnotations().GetName())
	observedTask4 := testutils.WatchTaskUpdate(t, watch)
	assert.Equal(t, api.TaskState_COMPLETE, observedTask4.Status.GetState())
	assert.Equal(t, "original", observedTask4.GetServiceAnnotations().GetName())

	// Delete the service.
	err = s.Update(func(tx store.Tx) error {
		assert.NoError(t, store.DeleteService(tx, service1.Id))
		return nil
	})

	// Service delete should trigger both the task desired statuses
	// to be set to REMOVE.
	observedTask3 = testutils.WatchTaskUpdate(t, watch)
	assert.Equal(t, api.TaskState_REMOVE, observedTask3.DesiredState)
	assert.Equal(t, "original", observedTask3.GetServiceAnnotations().GetName())
	observedTask4 = testutils.WatchTaskUpdate(t, watch)
	assert.Equal(t, api.TaskState_REMOVE, observedTask4.DesiredState)
	assert.Equal(t, "original", observedTask4.GetServiceAnnotations().GetName())

	testutils.Expect(t, watch, state.EventCommit{})

	// Task reaper should see the event updates for desired state update
	// to REMOVE and should deleted by the reaper.
	deletedTask1 := testutils.WatchTaskDelete(t, watch)
	assert.Equal(t, api.TaskState_COMPLETE, deletedTask1.Status.GetState())
	assert.Equal(t, "original", deletedTask1.GetServiceAnnotations().GetName())
	deletedTask2 := testutils.WatchTaskDelete(t, watch)
	assert.Equal(t, api.TaskState_COMPLETE, deletedTask2.Status.GetState())
	assert.Equal(t, "original", deletedTask2.GetServiceAnnotations().GetName())

	var foundTasks []*api.Task
	s.View(func(tx store.ReadTx) {
		foundTasks, err = store.FindTasks(tx, store.All)
	})
	assert.NoError(t, err)
	assert.Len(t, foundTasks, 0)
}

// TestTaskReaperBatching tests that the batching logic for the task reaper
// runs correctly.
func TestTaskReaperBatching(t *testing.T) {
	// create a canned context and store to use with this task reaper
	ctx := context.Background()
	s := store.NewMemoryStore(nil)
	assert.NotNil(t, s)
	defer s.Close()

	var (
		task1, task2, task3 *api.Task
		tasks               []*api.Task
	)

	// set up all of the test fixtures
	assert.NoError(t, s.Update(func(tx store.Tx) error {
		// we need a cluster object, because we need to set the retention limit
		// to a low value
		assert.NoError(t, store.CreateCluster(tx, &api.Cluster{
			Id: identity.NewID(),
			Spec: &api.ClusterSpec{
				Annotations: &api.Annotations{
					Name: store.DefaultClusterName,
				},
				Orchestration: &api.OrchestrationConfig{
					TaskHistoryRetentionLimit: 1,
				},
			},
		}))

		task1 = &api.Task{
			Id:           "foo",
			ServiceId:    "bar",
			Slot:         0,
			DesiredState: api.TaskState_SHUTDOWN,
			Status: &api.TaskStatus{
				State: api.TaskState_SHUTDOWN,
			},
		}
		// we need to create all of the tasks used in this test, because we'll
		// be using task update events to trigger reaper behavior.
		assert.NoError(t, store.CreateTask(tx, task1))

		task2 = &api.Task{
			Id:           "foo2",
			ServiceId:    "bar",
			Slot:         1,
			DesiredState: api.TaskState_SHUTDOWN,
			Status: &api.TaskStatus{
				State: api.TaskState_SHUTDOWN,
			},
		}
		assert.NoError(t, store.CreateTask(tx, task2))

		tasks = make([]*api.Task, maxDirty+1)
		for i := range maxDirty + 1 {
			tasks[i] = &api.Task{
				Id:        fmt.Sprintf("baz%v", i),
				ServiceId: "bar",
				// every task in a different slot, so they don't get cleaned up
				// based on exceeding the retention limit
				Slot:         uint64(i),
				DesiredState: api.TaskState_SHUTDOWN,
				Status: &api.TaskStatus{
					State: api.TaskState_SHUTDOWN,
				},
			}
			if err := store.CreateTask(tx, tasks[i]); err != nil {
				return err
			}
		}

		task3 = &api.Task{
			Id:           "foo3",
			ServiceId:    "bar",
			Slot:         2,
			DesiredState: api.TaskState_SHUTDOWN,
			Status: &api.TaskStatus{
				State: api.TaskState_SHUTDOWN,
			},
		}
		assert.NoError(t, store.CreateTask(tx, task3))
		return nil
	}))

	// now create the task reaper
	taskReaper := New(s)
	taskReaper.tickSignal = make(chan struct{}, 1)
	defer taskReaper.Stop()
	testutils.EnsureRuns(func() { taskReaper.Run(ctx) })

	// None of the tasks we've created are eligible for deletion. We should
	// see no task delete events. Wait for a tick signal, or 500ms to pass, to
	// verify that no tick will occur.
	select {
	case <-taskReaper.tickSignal:
		t.Fatalf("the taskreaper ticked when it should not have")
	case <-time.After(reaperBatchingInterval * 2):
		// ok, looks good, moving on
	}

	// update task1 to die
	assert.NoError(t, s.Update(func(tx store.Tx) error {
		task1.DesiredState = api.TaskState_REMOVE
		return store.UpdateTask(tx, task1)
	}))

	// the task should be added to the cleanup map and a tick should occur
	// shortly. give it an extra 50ms for overhead
	select {
	case <-taskReaper.tickSignal:
	case <-time.After(reaperBatchingInterval + (50 * time.Millisecond)):
		t.Fatalf("the taskreaper should have ticked but did not")
	}

	// now wait and make sure the task reaper does not tick again
	select {
	case <-taskReaper.tickSignal:
		t.Fatalf("the taskreaper should not have ticked but did")
	case <-time.After(reaperBatchingInterval * 2):
	}

	// now make sure we'll tick again if we update another task to die
	assert.NoError(t, s.Update(func(tx store.Tx) error {
		task2.DesiredState = api.TaskState_REMOVE
		return store.UpdateTask(tx, task2)
	}))

	select {
	case <-taskReaper.tickSignal:
	case <-time.After(reaperBatchingInterval + (50 * time.Millisecond)):
		t.Fatalf("the taskreaper should have ticked by now but did not")
	}

	// again, now wait and make sure the task reaper does not tick again
	select {
	case <-taskReaper.tickSignal:
		t.Fatalf("the taskreaper should not have ticked but did")
	case <-time.After(reaperBatchingInterval * 2):
	}

	// now create a shitload of tasks. this should tick immediately after, no
	// waiting. we should easily within the batching interval be able to
	// process all of these events, and should expect 1 tick immediately after
	// and no more
	assert.NoError(t, s.Update(func(tx store.Tx) error {
		for _, task := range tasks {
			task.DesiredState = api.TaskState_REMOVE
			assert.NoError(t, store.UpdateTask(tx, task))
		}
		return nil
	}))

	select {
	case <-taskReaper.tickSignal:
	case <-time.After(reaperBatchingInterval):
		// tight bound on the how long it should take to tick. we should tick
		// before the reaper batching interval. this should only POSSIBLY fail
		// on a really slow system, where processing the 1000+ incoming events
		// takes longer than the reaperBatchingInterval. if this test flakes
		// here, that's probably why.
		t.Fatalf("we should have immediately ticked already, but did not")
	}

	// again again, wait and make sure the task reaper does not tick again
	select {
	case <-taskReaper.tickSignal:
		t.Fatalf("the taskreaper should not have ticked but did")
	case <-time.After(reaperBatchingInterval * 2):
	}

	// now before we wrap up, make sure the task reaper still works off the
	// timer
	assert.NoError(t, s.Update(func(tx store.Tx) error {
		task3.DesiredState = api.TaskState_REMOVE
		return store.UpdateTask(tx, task3)
	}))

	select {
	case <-taskReaper.tickSignal:
	case <-time.After(reaperBatchingInterval + (50 * time.Millisecond)):
		t.Fatalf("the taskreaper should have ticked by now but did not")
	}

	// again, now wait and make sure the task reaper does not tick again
	select {
	case <-taskReaper.tickSignal:
		t.Fatalf("the taskreaper should not have ticked but did")
	case <-time.After(reaperBatchingInterval * 2):
	}
}

// TestServiceRemoveDeadTasks tests removal of
// tasks in state < TaskStateAssigned.
func TestServiceRemoveUnassignedTasks(t *testing.T) {
	ctx := context.Background()
	s := store.NewMemoryStore(nil)
	assert.NotNil(t, s)
	defer s.Close()

	assert.NoError(t, s.Update(func(tx store.Tx) error {
		store.CreateCluster(tx, &api.Cluster{
			Id: identity.NewID(),
			Spec: &api.ClusterSpec{
				Annotations: &api.Annotations{
					Name: store.DefaultClusterName,
				},
				Orchestration: &api.OrchestrationConfig{
					// set TaskHistoryRetentionLimit to a negative value, so
					// that tasks are cleaned up right away.
					TaskHistoryRetentionLimit: 1,
				},
			},
		})
		return nil
	}))

	taskReaper := New(s)
	defer taskReaper.Stop()
	orchestrator := replicated.NewReplicatedOrchestrator(s)
	defer orchestrator.Stop()

	watch, cancel := state.Watch(s.WatchQueue() /*api.EventCreateTask{}, api.EventUpdateTask{}*/)
	defer cancel()

	service1 := &api.Service{
		Id: "id1",
		Spec: &api.ServiceSpec{
			Annotations: &api.Annotations{
				Name: "name1",
			},
			Mode: &api.ServiceSpec_Replicated{
				Replicated: &api.ReplicatedService{
					Replicas: 1,
				},
			},
			Task: &api.TaskSpec{
				Restart: &api.RestartPolicy{
					// Turn off restart to get an accurate count on tasks.
					Condition: api.RestartPolicy_NONE,
					Delay:     durationpb.New(0),
				},
			},
		},
	}

	// Create a service with one replica specified before the orchestrator is
	// started. This should result in one tasks when the orchestrator
	// starts up.
	err := s.Update(func(tx store.Tx) error {
		assert.NoError(t, store.CreateService(tx, service1))
		return nil
	})
	assert.NoError(t, err)

	// Start the orchestrator.
	testutils.EnsureRuns(func() {
		assert.NoError(t, orchestrator.Run(ctx))
	})
	testutils.EnsureRuns(func() { taskReaper.Run(ctx) })

	observedTask1 := testutils.WatchTaskCreate(t, watch)
	assert.Equal(t, api.TaskState_NEW, observedTask1.Status.GetState())
	assert.Equal(t, observedTask1.GetServiceAnnotations().GetName(), "name1")

	// Set the task state to PENDING to simulate allocation.
	updatedTask1 := observedTask1.Copy()
	updatedTask1.Status.State = api.TaskState_PENDING
	updatedTask1.ServiceAnnotations = &api.Annotations{Name: "original"}
	err = s.Update(func(tx store.Tx) error {
		assert.NoError(t, store.UpdateTask(tx, updatedTask1))
		return nil
	})
	require.NoError(t, err)

	testutils.Expect(t, watch, state.EventCommit{})
	testutils.Expect(t, watch, api.EventUpdateTask{})
	testutils.Expect(t, watch, state.EventCommit{})

	service1.Spec.GetTask().ForceUpdate++
	// This should shutdown the previous task and create a new one.
	err = s.Update(func(tx store.Tx) error {
		assert.NoError(t, store.UpdateService(tx, service1))
		return nil
	})
	testutils.Expect(t, watch, api.EventUpdateService{})
	testutils.Expect(t, watch, state.EventCommit{})

	// New task should be created and old task marked for SHUTDOWN.
	observedTask1 = testutils.WatchTaskCreate(t, watch)
	assert.Equal(t, api.TaskState_NEW, observedTask1.Status.GetState())
	assert.Equal(t, observedTask1.GetServiceAnnotations().GetName(), "name1")

	observedTask3 := testutils.WatchTaskUpdate(t, watch)
	assert.Equal(t, api.TaskState_SHUTDOWN, observedTask3.DesiredState)
	assert.Equal(t, "original", observedTask3.GetServiceAnnotations().GetName())

	testutils.Expect(t, watch, state.EventCommit{})

	// Task reaper should delete the task previously marked for SHUTDOWN.
	deletedTask1 := testutils.WatchTaskDelete(t, watch)
	assert.Equal(t, api.TaskState_PENDING, deletedTask1.Status.GetState())
	assert.Equal(t, "original", deletedTask1.GetServiceAnnotations().GetName())

	testutils.Expect(t, watch, state.EventCommit{})

	var foundTasks []*api.Task
	s.View(func(tx store.ReadTx) {
		foundTasks, err = store.FindTasks(tx, store.All)
	})
	assert.NoError(t, err)
	assert.Len(t, foundTasks, 1)
}

// setupTaskReaperDirty adds slots to the task reaper dirty set for testing.
func setupTaskReaperDirty(tr *TaskReaper) {
	tr.dirty[orchestrator.SlotTuple{
		Slot:      1,
		ServiceID: "id1",
		NodeID:    "node1",
	}] = struct{}{}
	tr.dirty[orchestrator.SlotTuple{
		Slot:      1,
		ServiceID: "id2",
		NodeID:    "node1",
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
		Id: "id1",
		Spec: &api.ServiceSpec{
			Annotations: &api.Annotations{
				Name: "name1",
			},
			Mode: &api.ServiceSpec_Replicated{
				Replicated: &api.ReplicatedService{
					Replicas: 1,
				},
			},
			Task: &api.TaskSpec{
				Restart: &api.RestartPolicy{
					// Turn off restart to get an accurate count on tasks.
					Condition: api.RestartPolicy_NONE,
					Delay:     durationpb.New(0),
				},
			},
		},
	}

	// Create another service in the store for the following test cases.
	service2 := &api.Service{
		Id: "id2",
		Spec: &api.ServiceSpec{
			Annotations: &api.Annotations{
				Name: "name2",
			},
			Mode: &api.ServiceSpec_Replicated{
				Replicated: &api.ReplicatedService{
					Replicas: 1,
				},
			},
			Task: &api.TaskSpec{
				Restart: &api.RestartPolicy{
					// Turn off restart to get an accurate count on tasks.
					Condition: api.RestartPolicy_NONE,
					Delay:     durationpb.New(0),
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
		Id:           "id1task1",
		Slot:         1,
		DesiredState: api.TaskState_SHUTDOWN,
		Status: &api.TaskStatus{
			State: api.TaskState_SHUTDOWN,
		},
		ServiceId: "id1",
		ServiceAnnotations: &api.Annotations{
			Name: "name1",
		},
	}

	task2 := &api.Task{
		Id:           "id2task1",
		Slot:         1,
		DesiredState: api.TaskState_SHUTDOWN,
		Status: &api.TaskStatus{
			State: api.TaskState_SHUTDOWN,
		},
		ServiceId: "id2",
		ServiceAnnotations: &api.Annotations{
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
	task1.Status.State = api.TaskState_NEW
	task1.DesiredState = api.TaskState_RUNNING
	task1.Id = "id1task2"
	task2.Status.State = api.TaskState_NEW
	task2.DesiredState = api.TaskState_RUNNING
	task2.Id = "id2task2"
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
	assert.Equal(t, api.TaskState_SHUTDOWN, deletedTask1.Status.GetState())
	assert.Equal(t, api.TaskState_SHUTDOWN, deletedTask1.DesiredState)
	assert.True(t, deletedTask1.GetServiceAnnotations().GetName() == "name1" ||
		deletedTask1.GetServiceAnnotations().GetName() == "name2")

	deletedTask2 := testutils.WatchTaskDelete(t, watch)
	assert.Equal(t, api.TaskState_SHUTDOWN, deletedTask2.Status.GetState())
	assert.Equal(t, api.TaskState_SHUTDOWN, deletedTask2.DesiredState)
	assert.True(t, deletedTask1.GetServiceAnnotations().GetName() == "name1" ||
		deletedTask1.GetServiceAnnotations().GetName() == "name2")
}

// TestTickHistoryCleanup tests the condition the task reaper
// uses to delete historic tasks:
// 1. task in terminal state i.e. actual state > running
// 2. actual State < assigned and desired state > running.
func TestTickHistoryCleanup(t *testing.T) {
	s := store.NewMemoryStore(nil)
	assert.NotNil(t, s)
	defer s.Close()
	// Create a service.
	service1 := &api.Service{
		Id: "id1",
		Spec: &api.ServiceSpec{
			Annotations: &api.Annotations{
				Name: "name1",
			},
			Mode: &api.ServiceSpec_Replicated{
				Replicated: &api.ReplicatedService{
					Replicas: 1,
				},
			},
			Task: &api.TaskSpec{
				Restart: &api.RestartPolicy{
					// Turn off restart to get an accurate count on tasks.
					Condition: api.RestartPolicy_NONE,
					Delay:     durationpb.New(0),
				},
			},
		},
	}

	s.Update(func(tx store.Tx) error {
		assert.NoError(t, store.CreateService(tx, service1))
		return nil
	})

	watch, cancel := state.Watch(s.WatchQueue() /*api.EventDeleteTask{}*/)
	defer cancel()
	taskReaper := New(s)
	taskReaper.taskHistory = 0

	// Test function will create a task with the given desired and actual state,
	// setup the task reaper dirty list and call tick for testing.
	testfunc := func(desiredState api.TaskState, actualState api.TaskState) {
		var task *api.Task
		s.View(func(tx store.ReadTx) {
			task = store.GetTask(tx, "id1task3")
		})

		if task == nil {
			// create task3
			task3 := &api.Task{
				Id:           "id1task3",
				Slot:         1,
				DesiredState: desiredState,
				Status: &api.TaskStatus{
					State: actualState,
				},
				ServiceId: "id1",
				ServiceAnnotations: &api.Annotations{
					Name: "name1",
				},
			}
			s.Update(func(tx store.Tx) error {
				assert.NoError(t, store.CreateTask(tx, task3))
				return nil
			})
		} else {
			task.DesiredState = desiredState
			task.Status.State = actualState
			s.Update(func(tx store.Tx) error {
				assert.NoError(t, store.UpdateTask(tx, task))
				return nil
			})
		}

		setupTaskReaperDirty(taskReaper)
		taskReaper.tick()
	}

	// Function to verify task was deleted.
	waitForTaskDelete := func(desiredState api.TaskState, actualState api.TaskState) {
		deletedTask1 := testutils.WatchTaskDelete(t, watch)
		assert.Equal(t, actualState, deletedTask1.Status.GetState())
		assert.Equal(t, desiredState, deletedTask1.DesiredState)
		assert.Equal(t, "name1", deletedTask1.GetServiceAnnotations().GetName())
		assert.Equal(t, "id1task3", deletedTask1.Id)
	}

	for _, testcase := range []struct {
		// Desired and actual states to test.
		desired, actual api.TaskState

		// Flag to indicate whether the task should have been deleted by tick().
		cleanedUp bool
	}{
		{desired: api.TaskState_RUNNING, actual: api.TaskState_NEW, cleanedUp: false},
		{desired: api.TaskState_RUNNING, actual: api.TaskState_PENDING, cleanedUp: false},
		{desired: api.TaskState_RUNNING, actual: api.TaskState_ASSIGNED, cleanedUp: false},
		{desired: api.TaskState_RUNNING, actual: api.TaskState_ACCEPTED, cleanedUp: false},
		{desired: api.TaskState_RUNNING, actual: api.TaskState_PREPARING, cleanedUp: false},
		{desired: api.TaskState_RUNNING, actual: api.TaskState_READY, cleanedUp: false},
		{desired: api.TaskState_RUNNING, actual: api.TaskState_STARTING, cleanedUp: false},
		{desired: api.TaskState_RUNNING, actual: api.TaskState_RUNNING, cleanedUp: false},
		{desired: api.TaskState_RUNNING, actual: api.TaskState_COMPLETE, cleanedUp: true},
		{desired: api.TaskState_RUNNING, actual: api.TaskState_FAILED, cleanedUp: true},
		{desired: api.TaskState_RUNNING, actual: api.TaskState_REJECTED, cleanedUp: true},
		{desired: api.TaskState_RUNNING, actual: api.TaskState_REMOVE, cleanedUp: true},
		{desired: api.TaskState_RUNNING, actual: api.TaskState_ORPHANED, cleanedUp: true},

		{desired: api.TaskState_SHUTDOWN, actual: api.TaskState_NEW, cleanedUp: true},
		{desired: api.TaskState_SHUTDOWN, actual: api.TaskState_PENDING, cleanedUp: true},
		{desired: api.TaskState_SHUTDOWN, actual: api.TaskState_ASSIGNED, cleanedUp: false},
		{desired: api.TaskState_SHUTDOWN, actual: api.TaskState_ACCEPTED, cleanedUp: false},
		{desired: api.TaskState_SHUTDOWN, actual: api.TaskState_PREPARING, cleanedUp: false},
		{desired: api.TaskState_SHUTDOWN, actual: api.TaskState_READY, cleanedUp: false},
		{desired: api.TaskState_SHUTDOWN, actual: api.TaskState_STARTING, cleanedUp: false},
		{desired: api.TaskState_SHUTDOWN, actual: api.TaskState_RUNNING, cleanedUp: false},
		{desired: api.TaskState_SHUTDOWN, actual: api.TaskState_COMPLETE, cleanedUp: true},
		{desired: api.TaskState_SHUTDOWN, actual: api.TaskState_FAILED, cleanedUp: true},
		{desired: api.TaskState_SHUTDOWN, actual: api.TaskState_REJECTED, cleanedUp: true},
		{desired: api.TaskState_SHUTDOWN, actual: api.TaskState_REMOVE, cleanedUp: true},
		{desired: api.TaskState_SHUTDOWN, actual: api.TaskState_ORPHANED, cleanedUp: true},
	} {
		testfunc(testcase.desired, testcase.actual)
		assert.Zero(t, len(taskReaper.dirty))
		if testcase.cleanedUp {
			waitForTaskDelete(testcase.desired, testcase.actual)
		}
		s.View(func(tx store.ReadTx) {
			task := store.GetTask(tx, "id1task3")
			if testcase.cleanedUp {
				assert.Nil(t, task)
			} else {
				assert.NotNil(t, task)
			}
		})
	}
}
