package taskreaper

import (
	"fmt"
	"time"

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

// TestTaskReaperBatching tests that the batching logic for the task reaper
// runs correctly.
func TestTaskReaperBatching(t *testing.T) {
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
			ID: identity.NewID(),
			Spec: api.ClusterSpec{
				Annotations: api.Annotations{
					Name: store.DefaultClusterName,
				},
				Orchestration: api.OrchestrationConfig{
					// Retain no tasks, which will cause dead slots to be
					// deleted immediately
					TaskHistoryRetentionLimit: 0,
				},
			},
		}))

		task1 = &api.Task{
			ID:           "foo",
			ServiceID:    "bar",
			Slot:         0,
			DesiredState: api.TaskStateShutdown,
			Status: api.TaskStatus{
				State: api.TaskStateRunning,
			},
		}
		// we need to create all of the tasks used in this test, because we'll
		// be using task update events to trigger reaper behavior.
		assert.NoError(t, store.CreateTask(tx, task1))

		task2 = &api.Task{
			ID:           "foo2",
			ServiceID:    "bar",
			Slot:         1,
			DesiredState: api.TaskStateShutdown,
			Status: api.TaskStatus{
				State: api.TaskStateRunning,
			},
		}
		assert.NoError(t, store.CreateTask(tx, task2))

		tasks = make([]*api.Task, maxDirty+1)
		for i := 0; i < maxDirty+1; i++ {
			tasks[i] = &api.Task{
				ID:        fmt.Sprintf("baz%v", i),
				ServiceID: "bar",
				// every task in a different slot, so they don't get cleaned up
				// based on exceeding the retention limit
				Slot:         uint64(i),
				DesiredState: api.TaskStateShutdown,
				Status: api.TaskStatus{
					State: api.TaskStateRunning,
				},
			}
			if err := store.CreateTask(tx, tasks[i]); err != nil {
				return err
			}
		}

		task3 = &api.Task{
			ID:           "foo3",
			ServiceID:    "bar",
			Slot:         2,
			DesiredState: api.TaskStateShutdown,
			Status: api.TaskStatus{
				State: api.TaskStateRunning,
			},
		}
		assert.NoError(t, store.CreateTask(tx, task3))
		return nil
	}))

	// now create the task reaper
	taskReaper := New(s)
	taskReaper.tickSignal = make(chan struct{}, 1)
	defer taskReaper.Stop()
	go taskReaper.Run()

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
		task1.Status.State = api.TaskStateShutdown
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
		task2.Status.State = api.TaskStateShutdown
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
			task.Status.State = api.TaskStateShutdown
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
		task3.Status.State = api.TaskStateShutdown
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
