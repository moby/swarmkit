package orchestrator

import (
	"testing"
	"time"

	"github.com/docker/go-events"
	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/manager/state"
	"github.com/docker/swarm-v2/manager/state/store"
	"github.com/stretchr/testify/assert"
	"golang.org/x/net/context"
)

func TestOrchestrator(t *testing.T) {
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

	// Create a second service.
	err = store.Update(func(tx state.Tx) error {
		j2 := &api.Service{
			ID: "id2",
			Spec: api.ServiceSpec{
				Annotations: api.Annotations{
					Name: "name2",
				},
				Template:  &api.TaskSpec{},
				Instances: 1,
				Mode:      api.ServiceModeRunning,
			},
		}
		assert.NoError(t, tx.Services().Create(j2))
		return nil
	})
	assert.NoError(t, err)

	observedTask3 := watchTaskCreate(t, watch)
	assert.Equal(t, observedTask3.Status.State, api.TaskStateNew)
	assert.Equal(t, observedTask3.Annotations.Name, "name2")

	// Update a service to scale it out to 3 instances
	err = store.Update(func(tx state.Tx) error {
		j2 := &api.Service{
			ID: "id2",
			Spec: api.ServiceSpec{
				Annotations: api.Annotations{
					Name: "name2",
				},
				Template:  &api.TaskSpec{},
				Instances: 3,
				Mode:      api.ServiceModeRunning,
			},
		}
		assert.NoError(t, tx.Services().Update(j2))
		return nil
	})
	assert.NoError(t, err)

	observedTask4 := watchTaskCreate(t, watch)
	assert.Equal(t, observedTask4.Status.State, api.TaskStateNew)
	assert.Equal(t, observedTask4.Annotations.Name, "name2")

	observedTask5 := watchTaskCreate(t, watch)
	assert.Equal(t, observedTask5.Status.State, api.TaskStateNew)
	assert.Equal(t, observedTask5.Annotations.Name, "name2")

	// Now scale it back down to 1 instance
	err = store.Update(func(tx state.Tx) error {
		j2 := &api.Service{
			ID: "id2",
			Spec: api.ServiceSpec{
				Annotations: api.Annotations{
					Name: "name2",
				},
				Template:  &api.TaskSpec{},
				Instances: 1,
				Mode:      api.ServiceModeRunning,
			},
		}
		assert.NoError(t, tx.Services().Update(j2))
		return nil
	})
	assert.NoError(t, err)

	observedDeletion1 := watchDeadTask(t, watch)
	assert.Equal(t, observedDeletion1.Status.State, api.TaskStateNew)
	assert.Equal(t, observedDeletion1.Annotations.Name, "name2")

	observedDeletion2 := watchDeadTask(t, watch)
	assert.Equal(t, observedDeletion2.Status.State, api.TaskStateNew)
	assert.Equal(t, observedDeletion2.Annotations.Name, "name2")

	// There should be one remaining task attached to service id2/name2.
	var liveTasks []*api.Task
	store.View(func(readTx state.ReadTx) {
		var tasks []*api.Task
		tasks, err = readTx.Tasks().Find(state.ByServiceID("id2"))
		for _, t := range tasks {
			if t.DesiredState == api.TaskStateRunning {
				liveTasks = append(liveTasks, t)
			}
		}
	})
	assert.NoError(t, err)
	assert.Len(t, liveTasks, 1)

	// Delete the remaining task directly. It should be recreated by the
	// orchestrator.
	err = store.Update(func(tx state.Tx) error {
		assert.NoError(t, tx.Tasks().Delete(liveTasks[0].ID))
		return nil
	})
	assert.NoError(t, err)

	observedTask6 := watchTaskCreate(t, watch)
	assert.Equal(t, observedTask6.Status.State, api.TaskStateNew)
	assert.Equal(t, observedTask6.Annotations.Name, "name2")

	// Delete the service. Its remaining task should go away.
	err = store.Update(func(tx state.Tx) error {
		assert.NoError(t, tx.Services().Delete("id2"))
		return nil
	})
	assert.NoError(t, err)

	deletedTask := watchTaskDelete(t, watch)
	assert.Equal(t, deletedTask.Status.State, api.TaskStateNew)
	assert.Equal(t, deletedTask.Annotations.Name, "name2")

	orchestrator.Stop()
}

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

func watchTaskCreate(t *testing.T, watch chan events.Event) *api.Task {
	for {
		select {
		case event := <-watch:
			if task, ok := event.(state.EventCreateTask); ok {
				return task.Task
			}
			if _, ok := event.(state.EventUpdateTask); ok {
				t.Fatal("got EventUpdateTask when expecting EventCreateTask")
			}
		case <-time.After(time.Second):
			t.Fatal("no task assignment")
		}
	}
}

func watchTaskDelete(t *testing.T, watch chan events.Event) *api.Task {
	for {
		select {
		case event := <-watch:
			if task, ok := event.(state.EventDeleteTask); ok {
				return task.Task
			}
		case <-time.After(time.Second):
			t.Fatal("no task deletion")
		}
	}
}

func expectTaskUpdate(t *testing.T, watch chan events.Event) {
	for {
		select {
		case event := <-watch:
			if _, ok := event.(state.EventUpdateTask); !ok {
				t.Fatalf("expected task update event, got %#v", event)
			}
			return
		case <-time.After(time.Second):
			t.Fatal("no task update event")
		}
	}
}

func expectDeleteService(t *testing.T, watch chan events.Event) {
	for {
		select {
		case event := <-watch:
			if _, ok := event.(state.EventDeleteService); !ok {
				t.Fatalf("expected service delete event, got %#v", event)
			}
			return
		case <-time.After(time.Second):
			t.Fatal("no service delete event")
		}
	}
}

func expectCommit(t *testing.T, watch chan events.Event) {
	for {
		select {
		case event := <-watch:
			if _, ok := event.(state.EventCommit); !ok {
				t.Fatalf("expected commit event, got %#v", event)
			}
			return
		case <-time.After(time.Second):
			t.Fatal("no commit event")
		}
	}

}

func watchDeadTask(t *testing.T, watch chan events.Event) *api.Task {
	for {
		select {
		case event := <-watch:
			if task, ok := event.(state.EventUpdateTask); ok && task.Task.DesiredState == api.TaskStateDead {
				return task.Task
			}
			if _, ok := event.(state.EventCreateTask); ok {
				t.Fatal("got EventCreateTask when expecting EventUpdateTask")
			}
		case <-time.After(time.Second):
			t.Fatal("no task deletion")
		}
	}
}

func skipEvents(t *testing.T, watch chan events.Event) {
	for {
		select {
		case <-watch:
		case <-time.After(200 * time.Millisecond):
			return
		}
	}
}
