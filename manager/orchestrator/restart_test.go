package orchestrator

import (
	"testing"
	"time"

	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/manager/state"
	"github.com/docker/swarm-v2/manager/state/store"
	"github.com/stretchr/testify/assert"
	"golang.org/x/net/context"
)

func TestOrchestratorRestartAlways(t *testing.T) {
	ctx := context.Background()
	store := store.NewMemoryStore(nil)
	assert.NotNil(t, store)

	orchestrator := New(store)

	watch, cancel := state.Watch(store.WatchQueue() /*state.EventCreateTask{}, state.EventUpdateTask{}*/)
	defer cancel()

	// Create a service with two instances specified before the orchestrator is
	// started. This should result in two tasks when the orchestrator
	// starts up.
	err := store.Update(func(tx state.Tx) error {
		j1 := &api.Service{
			ID: "id1",
			Spec: api.ServiceSpec{
				Annotations: api.Annotations{
					Name: "name1",
				},
				Template:  &api.TaskSpec{},
				Instances: 2,
				Mode:      api.ServiceModeRunning,
				Restart: &api.RestartPolicy{
					Condition: api.RestartAlways,
				},
			},
		}
		assert.NoError(t, tx.Services().Create(j1))
		return nil
	})
	assert.NoError(t, err)

	// Start the orchestrator.
	go func() {
		assert.NoError(t, orchestrator.Run(ctx))
	}()

	observedTask1 := watchTaskCreate(t, watch)
	assert.Equal(t, observedTask1.Status.State, api.TaskStateNew)
	assert.Equal(t, observedTask1.Annotations.Name, "name1")

	observedTask2 := watchTaskCreate(t, watch)
	assert.Equal(t, observedTask2.Status.State, api.TaskStateNew)
	assert.Equal(t, observedTask2.Annotations.Name, "name1")

	// Fail the first task. Confirm that it gets restarted.
	updatedTask1 := observedTask1.Copy()
	updatedTask1.Status = api.TaskStatus{State: api.TaskStateDead, TerminalState: api.TaskStateFailed}
	err = store.Update(func(tx state.Tx) error {
		assert.NoError(t, tx.Tasks().Update(updatedTask1))
		return nil
	})
	assert.NoError(t, err)
	expectCommit(t, watch)
	expectTaskUpdate(t, watch)
	expectCommit(t, watch)
	expectTaskUpdate(t, watch)

	observedTask3 := watchTaskCreate(t, watch)
	assert.Equal(t, observedTask3.Status.State, api.TaskStateNew)
	assert.Equal(t, observedTask3.Annotations.Name, "name1")

	// Mark the second task as completed. Confirm that it gets restarted.
	updatedTask2 := observedTask2.Copy()
	updatedTask2.Status = api.TaskStatus{State: api.TaskStateDead, TerminalState: api.TaskStateCompleted}
	err = store.Update(func(tx state.Tx) error {
		assert.NoError(t, tx.Tasks().Update(updatedTask2))
		return nil
	})
	assert.NoError(t, err)
	expectCommit(t, watch)
	expectTaskUpdate(t, watch)
	expectCommit(t, watch)
	expectTaskUpdate(t, watch)

	observedTask4 := watchTaskCreate(t, watch)
	assert.Equal(t, observedTask4.Status.State, api.TaskStateNew)
	assert.Equal(t, observedTask4.Annotations.Name, "name1")

	orchestrator.Stop()
}

func TestOrchestratorRestartOnFailure(t *testing.T) {
	ctx := context.Background()
	store := store.NewMemoryStore(nil)
	assert.NotNil(t, store)

	orchestrator := New(store)

	watch, cancel := state.Watch(store.WatchQueue() /*state.EventCreateTask{}, state.EventUpdateTask{}*/)
	defer cancel()

	// Create a service with two instances specified before the orchestrator is
	// started. This should result in two tasks when the orchestrator
	// starts up.
	err := store.Update(func(tx state.Tx) error {
		j1 := &api.Service{
			ID: "id1",
			Spec: api.ServiceSpec{
				Annotations: api.Annotations{
					Name: "name1",
				},
				Template:  &api.TaskSpec{},
				Instances: 2,
				Mode:      api.ServiceModeRunning,
				Restart: &api.RestartPolicy{
					Condition: api.RestartOnFailure,
				},
			},
		}
		assert.NoError(t, tx.Services().Create(j1))
		return nil
	})
	assert.NoError(t, err)

	// Start the orchestrator.
	go func() {
		assert.NoError(t, orchestrator.Run(ctx))
	}()

	observedTask1 := watchTaskCreate(t, watch)
	assert.Equal(t, observedTask1.Status.State, api.TaskStateNew)
	assert.Equal(t, observedTask1.Annotations.Name, "name1")

	observedTask2 := watchTaskCreate(t, watch)
	assert.Equal(t, observedTask2.Status.State, api.TaskStateNew)
	assert.Equal(t, observedTask2.Annotations.Name, "name1")

	// Fail the first task. Confirm that it gets restarted.
	updatedTask1 := observedTask1.Copy()
	updatedTask1.Status.TerminalState = api.TaskStateFailed
	updatedTask1.Status = api.TaskStatus{State: api.TaskStateDead, TerminalState: api.TaskStateFailed}
	err = store.Update(func(tx state.Tx) error {
		assert.NoError(t, tx.Tasks().Update(updatedTask1))
		return nil
	})
	assert.NoError(t, err)
	expectCommit(t, watch)
	expectTaskUpdate(t, watch)
	expectCommit(t, watch)
	expectTaskUpdate(t, watch)

	observedTask3 := watchTaskCreate(t, watch)
	assert.Equal(t, observedTask3.Status.State, api.TaskStateNew)
	assert.Equal(t, observedTask3.Annotations.Name, "name1")

	// Mark the second task as completed. Confirm that it does not get restarted.
	updatedTask2 := observedTask2.Copy()
	updatedTask2.Status = api.TaskStatus{State: api.TaskStateDead, TerminalState: api.TaskStateCompleted}
	err = store.Update(func(tx state.Tx) error {
		assert.NoError(t, tx.Tasks().Update(updatedTask2))
		return nil
	})
	assert.NoError(t, err)
	expectCommit(t, watch)
	expectTaskUpdate(t, watch)
	expectCommit(t, watch)
	expectTaskUpdate(t, watch)
	expectCommit(t, watch)

	select {
	case <-watch:
		t.Fatal("got unexpected event")
	case <-time.After(100 * time.Millisecond):
	}

	orchestrator.Stop()
}

func TestOrchestratorRestartNever(t *testing.T) {
	ctx := context.Background()
	store := store.NewMemoryStore(nil)
	assert.NotNil(t, store)

	orchestrator := New(store)

	watch, cancel := state.Watch(store.WatchQueue() /*state.EventCreateTask{}, state.EventUpdateTask{}*/)
	defer cancel()

	// Create a service with two instances specified before the orchestrator is
	// started. This should result in two tasks when the orchestrator
	// starts up.
	err := store.Update(func(tx state.Tx) error {
		j1 := &api.Service{
			ID: "id1",
			Spec: api.ServiceSpec{
				Annotations: api.Annotations{
					Name: "name1",
				},
				Template:  &api.TaskSpec{},
				Instances: 2,
				Mode:      api.ServiceModeRunning,
				Restart: &api.RestartPolicy{
					Condition: api.RestartNever,
				},
			},
		}
		assert.NoError(t, tx.Services().Create(j1))
		return nil
	})
	assert.NoError(t, err)

	// Start the orchestrator.
	go func() {
		assert.NoError(t, orchestrator.Run(ctx))
	}()

	observedTask1 := watchTaskCreate(t, watch)
	assert.Equal(t, observedTask1.Status.State, api.TaskStateNew)
	assert.Equal(t, observedTask1.Annotations.Name, "name1")

	observedTask2 := watchTaskCreate(t, watch)
	assert.Equal(t, observedTask2.Status.State, api.TaskStateNew)
	assert.Equal(t, observedTask2.Annotations.Name, "name1")

	// Fail the first task. Confirm that it does not get restarted.
	updatedTask1 := observedTask1.Copy()
	updatedTask1.Status.TerminalState = api.TaskStateFailed
	updatedTask1.Status.State = api.TaskStateDead
	err = store.Update(func(tx state.Tx) error {
		assert.NoError(t, tx.Tasks().Update(updatedTask1))
		return nil
	})
	assert.NoError(t, err)
	expectCommit(t, watch)
	expectTaskUpdate(t, watch)
	expectCommit(t, watch)
	expectTaskUpdate(t, watch)
	expectCommit(t, watch)

	select {
	case <-watch:
		t.Fatal("got unexpected event")
	case <-time.After(100 * time.Millisecond):
	}

	// Mark the second task as completed. Confirm that it does not get restarted.
	updatedTask2 := observedTask2.Copy()
	updatedTask2.Status = api.TaskStatus{State: api.TaskStateDead, TerminalState: api.TaskStateCompleted}
	err = store.Update(func(tx state.Tx) error {
		assert.NoError(t, tx.Tasks().Update(updatedTask2))
		return nil
	})
	assert.NoError(t, err)
	expectTaskUpdate(t, watch)
	expectCommit(t, watch)
	expectTaskUpdate(t, watch)
	expectCommit(t, watch)

	select {
	case <-watch:
		t.Fatal("got unexpected event")
	case <-time.After(100 * time.Millisecond):
	}

	orchestrator.Stop()
}

func TestOrchestratorRestartDelay(t *testing.T) {
	ctx := context.Background()
	store := store.NewMemoryStore(nil)
	assert.NotNil(t, store)

	orchestrator := New(store)

	watch, cancel := state.Watch(store.WatchQueue() /*state.EventCreateTask{}, state.EventUpdateTask{}*/)
	defer cancel()

	// Create a service with two instances specified before the orchestrator is
	// started. This should result in two tasks when the orchestrator
	// starts up.
	err := store.Update(func(tx state.Tx) error {
		j1 := &api.Service{
			ID: "id1",
			Spec: api.ServiceSpec{
				Annotations: api.Annotations{
					Name: "name1",
				},
				Template:  &api.TaskSpec{},
				Instances: 2,
				Mode:      api.ServiceModeRunning,
				Restart: &api.RestartPolicy{
					Condition: api.RestartAlways,
					Delay:     100 * time.Millisecond,
				},
			},
		}
		assert.NoError(t, tx.Services().Create(j1))
		return nil
	})
	assert.NoError(t, err)

	// Start the orchestrator.
	go func() {
		assert.NoError(t, orchestrator.Run(ctx))
	}()

	observedTask1 := watchTaskCreate(t, watch)
	assert.Equal(t, observedTask1.Status.State, api.TaskStateNew)
	assert.Equal(t, observedTask1.Annotations.Name, "name1")

	observedTask2 := watchTaskCreate(t, watch)
	assert.Equal(t, observedTask2.Status.State, api.TaskStateNew)
	assert.Equal(t, observedTask2.Annotations.Name, "name1")

	// Fail the first task. Confirm that it gets restarted.
	updatedTask1 := observedTask1.Copy()
	updatedTask1.Status = api.TaskStatus{State: api.TaskStateDead, TerminalState: api.TaskStateFailed}
	before := time.Now()
	err = store.Update(func(tx state.Tx) error {
		assert.NoError(t, tx.Tasks().Update(updatedTask1))
		return nil
	})
	assert.NoError(t, err)
	expectCommit(t, watch)
	expectTaskUpdate(t, watch)
	expectCommit(t, watch)
	expectTaskUpdate(t, watch)

	observedTask3 := watchTaskCreate(t, watch)
	expectCommit(t, watch)
	assert.Equal(t, observedTask3.Status.State, api.TaskStateNew)
	assert.Equal(t, observedTask3.DesiredState, api.TaskStateReady)
	assert.Equal(t, observedTask3.Annotations.Name, "name1")

	var observedTask4 *api.Task

	select {
	case e := <-watch:
		if updateEvent, ok := e.(state.EventUpdateTask); ok {
			observedTask4 = updateEvent.Task
		} else {
			t.Fatal("got unexpected event")
		}
	case <-time.After(time.Second):
		t.Fatal("expected TaskUpdate event")
	}

	after := time.Now()

	// At least 100 ms should have elapsed. Only check the lower bound,
	// because the system may be slow and it could have taken longer.
	if after.Sub(before) < 100*time.Millisecond {
		t.Fatal("restart delay should have elapsed")
	}

	assert.Equal(t, observedTask4.Status.State, api.TaskStateNew)
	assert.Equal(t, observedTask4.DesiredState, api.TaskStateRunning)
	assert.Equal(t, observedTask4.Annotations.Name, "name1")

	orchestrator.Stop()
}
