package replicated

import (
	"context"
	"testing"
	"time"

	gogotypes "github.com/gogo/protobuf/types"
	"github.com/moby/swarmkit/v2/api"
	"github.com/moby/swarmkit/v2/manager/orchestrator/testutils"
	"github.com/moby/swarmkit/v2/manager/state"
	"github.com/moby/swarmkit/v2/manager/state/store"
	"github.com/moby/swarmkit/v2/protobuf/ptypes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOrchestratorRestartOnAny(t *testing.T) {
	ctx := context.Background()
	s := store.NewMemoryStore(nil)
	assert.NotNil(t, s)
	defer s.Close()

	orchestrator := NewReplicatedOrchestrator(s)
	defer orchestrator.Stop()

	watch, cancel := state.Watch(s.WatchQueue() /*api.EventCreateTask{}, api.EventUpdateTask{}*/)
	defer cancel()

	// Create a service with two instances specified before the orchestrator is
	// started. This should result in two tasks when the orchestrator
	// starts up.
	err := s.Update(func(tx store.Tx) error {
		j1 := &api.Service{
			ID: "id1",
			Spec: api.ServiceSpec{
				Annotations: api.Annotations{
					Name: "name1",
				},
				Task: api.TaskSpec{
					Runtime: &api.TaskSpec_Container{
						Container: &api.ContainerSpec{},
					},
					Restart: &api.RestartPolicy{
						Condition: api.RestartOnAny,
						Delay:     gogotypes.DurationProto(0),
					},
				},
				Mode: &api.ServiceSpec_Replicated{
					Replicated: &api.ReplicatedService{
						Replicas: 2,
					},
				},
			},
		}
		require.NoError(t, store.CreateService(tx, j1))
		return nil
	})
	require.NoError(t, err)

	// Start the orchestrator.
	go func() {
		require.NoError(t, orchestrator.Run(ctx))
	}()

	observedTask1 := testutils.WatchTaskCreate(t, watch)
	assert.Equal(t, api.TaskStateNew, observedTask1.Status.State)
	assert.Equal(t, "name1", observedTask1.ServiceAnnotations.Name)

	observedTask2 := testutils.WatchTaskCreate(t, watch)
	assert.Equal(t, api.TaskStateNew, observedTask2.Status.State)
	assert.Equal(t, "name1", observedTask2.ServiceAnnotations.Name)

	// Fail the first task. Confirm that it gets restarted.
	updatedTask1 := observedTask1.Copy()
	updatedTask1.Status = api.TaskStatus{State: api.TaskStateFailed, Timestamp: ptypes.MustTimestampProto(time.Now())}
	err = s.Update(func(tx store.Tx) error {
		require.NoError(t, store.UpdateTask(tx, updatedTask1))
		return nil
	})
	require.NoError(t, err)
	testutils.Expect(t, watch, state.EventCommit{})
	testutils.Expect(t, watch, api.EventUpdateTask{})
	testutils.Expect(t, watch, state.EventCommit{})
	testutils.Expect(t, watch, api.EventUpdateTask{})

	observedTask3 := testutils.WatchTaskCreate(t, watch)
	assert.Equal(t, api.TaskStateNew, observedTask3.Status.State)
	assert.Equal(t, "name1", observedTask3.ServiceAnnotations.Name)

	testutils.Expect(t, watch, state.EventCommit{})

	observedTask4 := testutils.WatchTaskUpdate(t, watch)
	assert.Equal(t, api.TaskStateRunning, observedTask4.DesiredState)
	assert.Equal(t, "name1", observedTask4.ServiceAnnotations.Name)

	// Mark the second task as completed. Confirm that it gets restarted.
	updatedTask2 := observedTask2.Copy()
	updatedTask2.Status = api.TaskStatus{State: api.TaskStateCompleted, Timestamp: ptypes.MustTimestampProto(time.Now())}
	err = s.Update(func(tx store.Tx) error {
		require.NoError(t, store.UpdateTask(tx, updatedTask2))
		return nil
	})
	require.NoError(t, err)
	testutils.Expect(t, watch, state.EventCommit{})
	testutils.Expect(t, watch, api.EventUpdateTask{})
	testutils.Expect(t, watch, state.EventCommit{})
	testutils.Expect(t, watch, api.EventUpdateTask{})

	observedTask5 := testutils.WatchTaskCreate(t, watch)
	assert.Equal(t, api.TaskStateNew, observedTask5.Status.State)
	assert.Equal(t, "name1", observedTask5.ServiceAnnotations.Name)

	testutils.Expect(t, watch, state.EventCommit{})

	observedTask6 := testutils.WatchTaskUpdate(t, watch)
	assert.Equal(t, api.TaskStateRunning, observedTask6.DesiredState)
	assert.Equal(t, "name1", observedTask6.ServiceAnnotations.Name)
}

func TestOrchestratorRestartOnFailure(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s := store.NewMemoryStore(nil)
	assert.NotNil(t, s)
	defer s.Close()

	orchestrator := NewReplicatedOrchestrator(s)
	defer orchestrator.Stop()

	watch, cancel := state.Watch(s.WatchQueue(), api.EventCreateTask{}, api.EventUpdateTask{})
	defer cancel()

	// Create a service with two instances specified before the orchestrator is
	// started. This should result in two tasks when the orchestrator
	// starts up.
	err := s.Update(func(tx store.Tx) error {
		j1 := &api.Service{
			ID: "id1",
			Spec: api.ServiceSpec{
				Annotations: api.Annotations{
					Name: "name1",
				},
				Task: api.TaskSpec{
					Runtime: &api.TaskSpec_Container{
						Container: &api.ContainerSpec{},
					},
					Restart: &api.RestartPolicy{
						Condition: api.RestartOnFailure,
						Delay:     gogotypes.DurationProto(0),
					},
				},
				Mode: &api.ServiceSpec_Replicated{
					Replicated: &api.ReplicatedService{
						Replicas: 2,
					},
				},
			},
		}
		require.NoError(t, store.CreateService(tx, j1))
		return nil
	})
	require.NoError(t, err)

	// Start the orchestrator.
	go func() {
		require.NoError(t, orchestrator.Run(ctx))
	}()

	observedTask1 := testutils.WatchTaskCreate(t, watch)
	assert.Equal(t, api.TaskStateNew, observedTask1.Status.State)
	assert.Equal(t, "name1", observedTask1.ServiceAnnotations.Name)

	observedTask2 := testutils.WatchTaskCreate(t, watch)
	assert.Equal(t, api.TaskStateNew, observedTask2.Status.State)
	assert.Equal(t, "name1", observedTask2.ServiceAnnotations.Name)

	// Fail the first task. Confirm that it gets restarted.
	updatedTask1 := observedTask1.Copy()
	updatedTask1.Status = api.TaskStatus{State: api.TaskStateFailed, Timestamp: ptypes.MustTimestampProto(time.Now())}
	err = s.Update(func(tx store.Tx) error {
		require.NoError(t, store.UpdateTask(tx, updatedTask1))
		return nil
	})
	require.NoError(t, err)
	testutils.Expect(t, watch, api.EventUpdateTask{})
	testutils.Expect(t, watch, api.EventUpdateTask{})

	observedTask3 := testutils.WatchTaskCreate(t, watch)
	assert.Equal(t, api.TaskStateNew, observedTask3.Status.State)
	assert.Equal(t, api.TaskStateReady, observedTask3.DesiredState)
	assert.Equal(t, "name1", observedTask3.ServiceAnnotations.Name)

	observedTask4 := testutils.WatchTaskUpdate(t, watch)
	assert.Equal(t, api.TaskStateRunning, observedTask4.DesiredState)
	assert.Equal(t, "name1", observedTask4.ServiceAnnotations.Name)

	// Mark the second task as completed. Confirm that it does not get restarted.
	updatedTask2 := observedTask2.Copy()
	updatedTask2.Status = api.TaskStatus{State: api.TaskStateCompleted, Timestamp: ptypes.MustTimestampProto(time.Now())}
	err = s.Update(func(tx store.Tx) error {
		require.NoError(t, store.UpdateTask(tx, updatedTask2))
		return nil
	})
	require.NoError(t, err)
	testutils.Expect(t, watch, api.EventUpdateTask{})
	testutils.Expect(t, watch, api.EventUpdateTask{})

	select {
	case <-watch:
		t.Fatal("got unexpected event")
	case <-time.After(100 * time.Millisecond):
	}

	// Update the service, but don't change anything in the spec. The
	// second instance instance should not be restarted.
	err = s.Update(func(tx store.Tx) error {
		service := store.GetService(tx, "id1")
		require.NotNil(t, service)
		require.NoError(t, store.UpdateService(tx, service))
		return nil
	})
	require.NoError(t, err)

	select {
	case <-watch:
		t.Fatal("got unexpected event")
	case <-time.After(100 * time.Millisecond):
	}

	// Update the service, and change the TaskSpec. Now the second instance
	// should be restarted.
	err = s.Update(func(tx store.Tx) error {
		service := store.GetService(tx, "id1")
		require.NotNil(t, service)
		service.Spec.Task.ForceUpdate++
		require.NoError(t, store.UpdateService(tx, service))
		return nil
	})
	require.NoError(t, err)
	testutils.Expect(t, watch, api.EventCreateTask{})
}

func TestOrchestratorRestartOnNone(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s := store.NewMemoryStore(nil)
	assert.NotNil(t, s)
	defer s.Close()

	orchestrator := NewReplicatedOrchestrator(s)
	defer orchestrator.Stop()

	watch, cancel := state.Watch(s.WatchQueue(), api.EventCreateTask{}, api.EventUpdateTask{})
	defer cancel()

	// Create a service with two instances specified before the orchestrator is
	// started. This should result in two tasks when the orchestrator
	// starts up.
	err := s.Update(func(tx store.Tx) error {
		j1 := &api.Service{
			ID: "id1",
			Spec: api.ServiceSpec{
				Annotations: api.Annotations{
					Name: "name1",
				},
				Task: api.TaskSpec{
					Runtime: &api.TaskSpec_Container{
						Container: &api.ContainerSpec{},
					},
					Restart: &api.RestartPolicy{
						Condition: api.RestartOnNone,
					},
				},
				Mode: &api.ServiceSpec_Replicated{
					Replicated: &api.ReplicatedService{
						Replicas: 2,
					},
				},
			},
		}
		require.NoError(t, store.CreateService(tx, j1))
		return nil
	})
	require.NoError(t, err)

	// Start the orchestrator.
	go func() {
		require.NoError(t, orchestrator.Run(ctx))
	}()

	observedTask1 := testutils.WatchTaskCreate(t, watch)
	assert.Equal(t, api.TaskStateNew, observedTask1.Status.State)
	assert.Equal(t, "name1", observedTask1.ServiceAnnotations.Name)

	observedTask2 := testutils.WatchTaskCreate(t, watch)
	assert.Equal(t, api.TaskStateNew, observedTask2.Status.State)
	assert.Equal(t, "name1", observedTask2.ServiceAnnotations.Name)

	// Fail the first task. Confirm that it does not get restarted.
	updatedTask1 := observedTask1.Copy()
	updatedTask1.Status.State = api.TaskStateFailed
	err = s.Update(func(tx store.Tx) error {
		require.NoError(t, store.UpdateTask(tx, updatedTask1))
		return nil
	})
	require.NoError(t, err)
	testutils.Expect(t, watch, api.EventUpdateTask{})
	testutils.Expect(t, watch, api.EventUpdateTask{})

	select {
	case <-watch:
		t.Fatal("got unexpected event")
	case <-time.After(100 * time.Millisecond):
	}

	// Mark the second task as completed. Confirm that it does not get restarted.
	updatedTask2 := observedTask2.Copy()
	updatedTask2.Status = api.TaskStatus{State: api.TaskStateCompleted, Timestamp: ptypes.MustTimestampProto(time.Now())}
	err = s.Update(func(tx store.Tx) error {
		require.NoError(t, store.UpdateTask(tx, updatedTask2))
		return nil
	})
	require.NoError(t, err)
	testutils.Expect(t, watch, api.EventUpdateTask{})
	testutils.Expect(t, watch, api.EventUpdateTask{})

	select {
	case <-watch:
		t.Fatal("got unexpected event")
	case <-time.After(100 * time.Millisecond):
	}

	// Update the service, but don't change anything in the spec. Neither
	// instance should be restarted.
	err = s.Update(func(tx store.Tx) error {
		service := store.GetService(tx, "id1")
		require.NotNil(t, service)
		require.NoError(t, store.UpdateService(tx, service))
		return nil
	})
	require.NoError(t, err)

	select {
	case <-watch:
		t.Fatal("got unexpected event")
	case <-time.After(100 * time.Millisecond):
	}

	// Update the service, and change the TaskSpec. Both instances should
	// be restarted.
	err = s.Update(func(tx store.Tx) error {
		service := store.GetService(tx, "id1")
		require.NotNil(t, service)
		service.Spec.Task.ForceUpdate++
		require.NoError(t, store.UpdateService(tx, service))
		return nil
	})
	require.NoError(t, err)
	testutils.Expect(t, watch, api.EventCreateTask{})
	newTask := testutils.WatchTaskUpdate(t, watch)
	assert.Equal(t, api.TaskStateRunning, newTask.DesiredState)
	err = s.Update(func(tx store.Tx) error {
		newTask := store.GetTask(tx, newTask.ID)
		require.NotNil(t, newTask)
		newTask.Status.State = api.TaskStateRunning
		require.NoError(t, store.UpdateTask(tx, newTask))
		return nil
	})
	require.NoError(t, err)
	testutils.Expect(t, watch, api.EventUpdateTask{})

	testutils.Expect(t, watch, api.EventCreateTask{})
}

func TestOrchestratorRestartDelay(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s := store.NewMemoryStore(nil)
	assert.NotNil(t, s)
	defer s.Close()

	orchestrator := NewReplicatedOrchestrator(s)
	defer orchestrator.Stop()

	watch, cancel := state.Watch(s.WatchQueue() /*api.EventCreateTask{}, api.EventUpdateTask{}*/)
	defer cancel()

	// Create a service with two instances specified before the orchestrator is
	// started. This should result in two tasks when the orchestrator
	// starts up.
	err := s.Update(func(tx store.Tx) error {
		j1 := &api.Service{
			ID: "id1",
			Spec: api.ServiceSpec{
				Annotations: api.Annotations{
					Name: "name1",
				},
				Task: api.TaskSpec{
					Runtime: &api.TaskSpec_Container{
						Container: &api.ContainerSpec{},
					},
					Restart: &api.RestartPolicy{
						Condition: api.RestartOnAny,
						Delay:     gogotypes.DurationProto(100 * time.Millisecond),
					},
				},
				Mode: &api.ServiceSpec_Replicated{
					Replicated: &api.ReplicatedService{
						Replicas: 2,
					},
				},
			},
		}
		require.NoError(t, store.CreateService(tx, j1))
		return nil
	})
	require.NoError(t, err)

	// Start the orchestrator.
	go func() {
		require.NoError(t, orchestrator.Run(ctx))
	}()

	observedTask1 := testutils.WatchTaskCreate(t, watch)
	assert.Equal(t, api.TaskStateNew, observedTask1.Status.State)
	assert.Equal(t, "name1", observedTask1.ServiceAnnotations.Name)

	observedTask2 := testutils.WatchTaskCreate(t, watch)
	assert.Equal(t, api.TaskStateNew, observedTask2.Status.State)
	assert.Equal(t, "name1", observedTask2.ServiceAnnotations.Name)

	// Fail the first task. Confirm that it gets restarted.
	updatedTask1 := observedTask1.Copy()
	updatedTask1.Status = api.TaskStatus{State: api.TaskStateFailed, Timestamp: ptypes.MustTimestampProto(time.Now())}
	before := time.Now()
	err = s.Update(func(tx store.Tx) error {
		require.NoError(t, store.UpdateTask(tx, updatedTask1))
		return nil
	})
	require.NoError(t, err)
	testutils.Expect(t, watch, state.EventCommit{})
	testutils.Expect(t, watch, api.EventUpdateTask{})
	testutils.Expect(t, watch, state.EventCommit{})
	testutils.Expect(t, watch, api.EventUpdateTask{})

	observedTask3 := testutils.WatchTaskCreate(t, watch)
	testutils.Expect(t, watch, state.EventCommit{})
	assert.Equal(t, api.TaskStateNew, observedTask3.Status.State)
	assert.Equal(t, api.TaskStateReady, observedTask3.DesiredState)
	assert.Equal(t, "name1", observedTask3.ServiceAnnotations.Name)

	observedTask4 := testutils.WatchTaskUpdate(t, watch)
	after := time.Now()

	// At least 100 ms should have elapsed. Only check the lower bound,
	// because the system may be slow and it could have taken longer.
	require.GreaterOrEqualf(t, after.Sub(before), 100*time.Millisecond, "restart delay should have elapsed. Got: %v", after.Sub(before))

	assert.Equal(t, api.TaskStateNew, observedTask4.Status.State)
	assert.Equal(t, api.TaskStateRunning, observedTask4.DesiredState)
	assert.Equal(t, "name1", observedTask4.ServiceAnnotations.Name)
}

func TestOrchestratorRestartMaxAttempts(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s := store.NewMemoryStore(nil)
	assert.NotNil(t, s)
	defer s.Close()

	orchestrator := NewReplicatedOrchestrator(s)
	defer orchestrator.Stop()

	watch, cancel := state.Watch(s.WatchQueue(), api.EventCreateTask{}, api.EventUpdateTask{})
	defer cancel()

	// Create a service with two instances specified before the orchestrator is
	// started. This should result in two tasks when the orchestrator
	// starts up.
	err := s.Update(func(tx store.Tx) error {
		j1 := &api.Service{
			ID: "id1",
			Spec: api.ServiceSpec{
				Annotations: api.Annotations{
					Name: "name1",
				},
				Mode: &api.ServiceSpec_Replicated{
					Replicated: &api.ReplicatedService{
						Replicas: 2,
					},
				},
				Task: api.TaskSpec{
					Runtime: &api.TaskSpec_Container{
						Container: &api.ContainerSpec{},
					},
					Restart: &api.RestartPolicy{
						Condition:   api.RestartOnAny,
						Delay:       gogotypes.DurationProto(100 * time.Millisecond),
						MaxAttempts: 1,
					},
				},
			},
			SpecVersion: &api.Version{
				Index: 1,
			},
		}
		require.NoError(t, store.CreateService(tx, j1))
		return nil
	})
	require.NoError(t, err)

	// Start the orchestrator.
	go func() {
		require.NoError(t, orchestrator.Run(ctx))
	}()

	failTask := func(task *api.Task, expectRestart bool) {
		task = task.Copy()
		task.Status = api.TaskStatus{State: api.TaskStateFailed, Timestamp: ptypes.MustTimestampProto(time.Now())}
		err = s.Update(func(tx store.Tx) error {
			require.NoError(t, store.UpdateTask(tx, task))
			return nil
		})
		require.NoError(t, err)
		testutils.Expect(t, watch, api.EventUpdateTask{})
		task = testutils.WatchShutdownTask(t, watch)
		if expectRestart {
			createdTask := testutils.WatchTaskCreate(t, watch)
			assert.Equal(t, api.TaskStateNew, createdTask.Status.State)
			assert.Equal(t, api.TaskStateReady, createdTask.DesiredState)
			assert.Equal(t, "name1", createdTask.ServiceAnnotations.Name)
		}
		err = s.Update(func(tx store.Tx) error {
			task := task.Copy()
			task.Status.State = api.TaskStateShutdown
			require.NoError(t, store.UpdateTask(tx, task))
			return nil
		})
		require.NoError(t, err)
		testutils.Expect(t, watch, api.EventUpdateTask{})
	}

	testRestart := func(serviceUpdated bool) {
		observedTask1 := testutils.WatchTaskCreate(t, watch)
		assert.Equal(t, api.TaskStateNew, observedTask1.Status.State)
		assert.Equal(t, "name1", observedTask1.ServiceAnnotations.Name)

		if serviceUpdated {
			runnableTask := testutils.WatchTaskUpdate(t, watch)
			assert.Equal(t, observedTask1.ID, runnableTask.ID)
			assert.Equal(t, api.TaskStateRunning, runnableTask.DesiredState)
			err = s.Update(func(tx store.Tx) error {
				task := runnableTask.Copy()
				task.Status.State = api.TaskStateRunning
				require.NoError(t, store.UpdateTask(tx, task))
				return nil
			})
			require.NoError(t, err)

			testutils.Expect(t, watch, api.EventUpdateTask{})
		}

		observedTask2 := testutils.WatchTaskCreate(t, watch)
		assert.Equal(t, api.TaskStateNew, observedTask2.Status.State)
		assert.Equal(t, "name1", observedTask2.ServiceAnnotations.Name)

		if serviceUpdated {
			testutils.Expect(t, watch, api.EventUpdateTask{})
		}

		// Fail the first task. Confirm that it gets restarted.
		before := time.Now()
		failTask(observedTask1, true)

		observedTask4 := testutils.WatchTaskUpdate(t, watch)
		after := time.Now()

		// At least 100 ms should have elapsed. Only check the lower bound,
		// because the system may be slow and it could have taken longer.
		require.GreaterOrEqual(t, after.Sub(before), 100*time.Millisecond, "restart delay should have elapsed")

		assert.Equal(t, api.TaskStateNew, observedTask4.Status.State)
		assert.Equal(t, api.TaskStateRunning, observedTask4.DesiredState)
		assert.Equal(t, "name1", observedTask4.ServiceAnnotations.Name)

		// Fail the second task. Confirm that it gets restarted.
		failTask(observedTask2, true)

		observedTask6 := testutils.WatchTaskUpdate(t, watch) // task gets started after a delay
		assert.Equal(t, api.TaskStateNew, observedTask6.Status.State)
		assert.Equal(t, api.TaskStateRunning, observedTask6.DesiredState)
		assert.Equal(t, "name1", observedTask6.ServiceAnnotations.Name)

		// Fail the first instance again. It should not be restarted.
		failTask(observedTask4, false)

		select {
		case <-watch:
			t.Fatal("got unexpected event")
		case <-time.After(200 * time.Millisecond):
		}

		// Fail the second instance again. It should not be restarted.
		failTask(observedTask6, false)

		select {
		case <-watch:
			t.Fatal("got unexpected event")
		case <-time.After(200 * time.Millisecond):
		}
	}

	testRestart(false)

	// Update the service spec
	err = s.Update(func(tx store.Tx) error {
		s := store.GetService(tx, "id1")
		require.NotNil(t, s)
		s.Spec.Task.GetContainer().Image = "newimage"
		s.SpecVersion.Index = 2
		require.NoError(t, store.UpdateService(tx, s))
		return nil
	})
	require.NoError(t, err)

	testRestart(true)
}

func TestOrchestratorRestartWindow(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s := store.NewMemoryStore(nil)
	assert.NotNil(t, s)
	defer s.Close()

	orchestrator := NewReplicatedOrchestrator(s)
	defer orchestrator.Stop()

	watch, cancel := state.Watch(s.WatchQueue() /*api.EventCreateTask{}, api.EventUpdateTask{}*/)
	defer cancel()

	// Create a service with two instances specified before the orchestrator is
	// started. This should result in two tasks when the orchestrator
	// starts up.
	err := s.Update(func(tx store.Tx) error {
		j1 := &api.Service{
			ID: "id1",
			Spec: api.ServiceSpec{
				Annotations: api.Annotations{
					Name: "name1",
				},
				Mode: &api.ServiceSpec_Replicated{
					Replicated: &api.ReplicatedService{
						Replicas: 2,
					},
				},
				Task: api.TaskSpec{
					Restart: &api.RestartPolicy{
						Condition:   api.RestartOnAny,
						Delay:       gogotypes.DurationProto(100 * time.Millisecond),
						MaxAttempts: 1,
						Window:      gogotypes.DurationProto(500 * time.Millisecond),
					},
				},
			},
		}
		require.NoError(t, store.CreateService(tx, j1))
		return nil
	})
	require.NoError(t, err)

	// Start the orchestrator.
	go func() {
		require.NoError(t, orchestrator.Run(ctx))
	}()

	observedTask1 := testutils.WatchTaskCreate(t, watch)
	assert.Equal(t, api.TaskStateNew, observedTask1.Status.State)
	assert.Equal(t, "name1", observedTask1.ServiceAnnotations.Name)

	observedTask2 := testutils.WatchTaskCreate(t, watch)
	assert.Equal(t, api.TaskStateNew, observedTask2.Status.State)
	assert.Equal(t, "name1", observedTask2.ServiceAnnotations.Name)

	// Fail the first task. Confirm that it gets restarted.
	updatedTask1 := observedTask1.Copy()
	updatedTask1.Status = api.TaskStatus{State: api.TaskStateFailed, Timestamp: ptypes.MustTimestampProto(time.Now())}
	before := time.Now()
	err = s.Update(func(tx store.Tx) error {
		require.NoError(t, store.UpdateTask(tx, updatedTask1))
		return nil
	})
	require.NoError(t, err)
	testutils.Expect(t, watch, state.EventCommit{})
	testutils.Expect(t, watch, api.EventUpdateTask{})
	testutils.Expect(t, watch, state.EventCommit{})
	testutils.Expect(t, watch, api.EventUpdateTask{})

	observedTask3 := testutils.WatchTaskCreate(t, watch)
	testutils.Expect(t, watch, state.EventCommit{})
	assert.Equal(t, api.TaskStateNew, observedTask3.Status.State)
	assert.Equal(t, api.TaskStateReady, observedTask3.DesiredState)
	assert.Equal(t, "name1", observedTask3.ServiceAnnotations.Name)

	observedTask4 := testutils.WatchTaskUpdate(t, watch)
	after := time.Now()

	// At least 100 ms should have elapsed. Only check the lower bound,
	// because the system may be slow and it could have taken longer.
	require.GreaterOrEqual(t, after.Sub(before), 100*time.Millisecond, "restart delay should have elapsed")

	assert.Equal(t, api.TaskStateNew, observedTask4.Status.State)
	assert.Equal(t, api.TaskStateRunning, observedTask4.DesiredState)
	assert.Equal(t, "name1", observedTask4.ServiceAnnotations.Name)

	// Fail the second task. Confirm that it gets restarted.
	updatedTask2 := observedTask2.Copy()
	updatedTask2.Status = api.TaskStatus{State: api.TaskStateFailed, Timestamp: ptypes.MustTimestampProto(time.Now())}
	err = s.Update(func(tx store.Tx) error {
		require.NoError(t, store.UpdateTask(tx, updatedTask2))
		return nil
	})
	require.NoError(t, err)
	testutils.Expect(t, watch, state.EventCommit{})
	testutils.Expect(t, watch, api.EventUpdateTask{})
	testutils.Expect(t, watch, state.EventCommit{})
	testutils.Expect(t, watch, api.EventUpdateTask{})

	observedTask5 := testutils.WatchTaskCreate(t, watch)
	testutils.Expect(t, watch, state.EventCommit{})
	assert.Equal(t, api.TaskStateNew, observedTask5.Status.State)
	assert.Equal(t, api.TaskStateReady, observedTask5.DesiredState)
	assert.Equal(t, "name1", observedTask5.ServiceAnnotations.Name)

	observedTask6 := testutils.WatchTaskUpdate(t, watch) // task gets started after a delay
	testutils.Expect(t, watch, state.EventCommit{})
	assert.Equal(t, api.TaskStateNew, observedTask6.Status.State)
	assert.Equal(t, api.TaskStateRunning, observedTask6.DesiredState)
	assert.Equal(t, "name1", observedTask6.ServiceAnnotations.Name)

	// Fail the first instance again. It should not be restarted.
	updatedTask1 = observedTask3.Copy()
	updatedTask1.Status = api.TaskStatus{State: api.TaskStateFailed, Timestamp: ptypes.MustTimestampProto(time.Now())}
	err = s.Update(func(tx store.Tx) error {
		require.NoError(t, store.UpdateTask(tx, updatedTask1))
		return nil
	})
	require.NoError(t, err)
	testutils.Expect(t, watch, api.EventUpdateTask{})
	testutils.Expect(t, watch, state.EventCommit{})
	testutils.Expect(t, watch, api.EventUpdateTask{})
	testutils.Expect(t, watch, state.EventCommit{})

	select {
	case <-watch:
		t.Fatal("got unexpected event")
	case <-time.After(200 * time.Millisecond):
	}

	time.Sleep(time.Second)

	// Fail the second instance again. It should get restarted because
	// enough time has elapsed since the last restarts.
	updatedTask2 = observedTask5.Copy()
	updatedTask2.Status = api.TaskStatus{State: api.TaskStateFailed, Timestamp: ptypes.MustTimestampProto(time.Now())}
	before = time.Now()
	err = s.Update(func(tx store.Tx) error {
		require.NoError(t, store.UpdateTask(tx, updatedTask2))
		return nil
	})
	require.NoError(t, err)
	testutils.Expect(t, watch, api.EventUpdateTask{})
	testutils.Expect(t, watch, state.EventCommit{})
	testutils.Expect(t, watch, api.EventUpdateTask{})

	observedTask7 := testutils.WatchTaskCreate(t, watch)
	testutils.Expect(t, watch, state.EventCommit{})
	assert.Equal(t, api.TaskStateNew, observedTask7.Status.State)
	assert.Equal(t, api.TaskStateReady, observedTask7.DesiredState)

	observedTask8 := testutils.WatchTaskUpdate(t, watch)
	after = time.Now()

	// At least 100 ms should have elapsed. Only check the lower bound,
	// because the system may be slow and it could have taken longer.
	require.GreaterOrEqual(t, after.Sub(before), 100*time.Millisecond, "restart delay should have elapsed")

	assert.Equal(t, api.TaskStateNew, observedTask8.Status.State)
	assert.Equal(t, api.TaskStateRunning, observedTask8.DesiredState)
	assert.Equal(t, "name1", observedTask8.ServiceAnnotations.Name)
}
