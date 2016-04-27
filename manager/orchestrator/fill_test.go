package orchestrator

import (
	"testing"

	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/manager/state"
	"github.com/docker/swarm-v2/manager/state/store"
	"github.com/stretchr/testify/assert"
	"golang.org/x/net/context"
)

var (
	node1 = &api.Node{
		ID:   "id1",
		Spec: &api.NodeSpec{},
		Description: &api.NodeDescription{
			Hostname: "name1",
		},
	}
	node2 = &api.Node{
		ID:   "id2",
		Spec: &api.NodeSpec{},
		Description: &api.NodeDescription{
			Hostname: "name2",
		},
	}

	service1 = &api.Service{
		ID: "id1",
		Spec: &api.ServiceSpec{
			Annotations: api.Annotations{
				Name: "name1",
			},
			Template:  &api.TaskSpec{},
			Instances: 1,
			Mode:      api.ServiceModeFill,
		},
	}

	service2 = &api.Service{
		ID: "id2",
		Spec: &api.ServiceSpec{
			Annotations: api.Annotations{
				Name: "name2",
			},
			Template:  &api.TaskSpec{},
			Instances: 1,
			Mode:      api.ServiceModeFill,
		},
	}
)

func SetupCluster(t *testing.T, store *store.MemoryStore) {
	ctx := context.Background()
	// Start the fill orchestrator.
	fill := NewFillOrchestrator(store)
	go func() {
		assert.NoError(t, fill.Run(ctx))
	}()

	addService(t, store, service1)
	addNode(t, store, node1)
}

func TestSetup(t *testing.T) {
	store := store.NewMemoryStore(nil)
	assert.NotNil(t, store)

	watch, cancel := state.Watch(store.WatchQueue() /*state.EventCreateTask{}, state.EventUpdateTask{}*/)
	defer cancel()

	SetupCluster(t, store)

	observedTask1 := watchTaskCreate(t, watch)
	assert.Equal(t, observedTask1.Status.State, api.TaskStateNew)
	assert.Equal(t, observedTask1.Annotations.Name, "name1")
	assert.Equal(t, observedTask1.NodeID, "id1")
}

func TestAddNode(t *testing.T) {
	store := store.NewMemoryStore(nil)
	assert.NotNil(t, store)

	SetupCluster(t, store)

	watch, cancel := state.Watch(store.WatchQueue())
	defer cancel()

	skipEvents(t, watch)

	addNode(t, store, node2)
	observedTask2 := watchTaskCreate(t, watch)
	assert.Equal(t, observedTask2.Status.State, api.TaskStateNew)
	assert.Equal(t, observedTask2.Annotations.Name, "name1")
	assert.Equal(t, observedTask2.NodeID, "id2")
}

func TestDeleteNode(t *testing.T) {
	store := store.NewMemoryStore(nil)
	assert.NotNil(t, store)

	SetupCluster(t, store)

	watch, cancel := state.Watch(store.WatchQueue())
	defer cancel()

	skipEvents(t, watch)

	deleteNode(t, store, node1)
	// task should be set to dead
	observedTask := watchDeadTask(t, watch)
	assert.Equal(t, observedTask.Annotations.Name, "name1")
	assert.Equal(t, observedTask.NodeID, "id1")
}

func TestAddService(t *testing.T) {
	store := store.NewMemoryStore(nil)
	assert.NotNil(t, store)

	SetupCluster(t, store)

	watch, cancel := state.Watch(store.WatchQueue())
	defer cancel()

	skipEvents(t, watch)

	addService(t, store, service2)
	observedTask := watchTaskCreate(t, watch)
	assert.Equal(t, observedTask.Status.State, api.TaskStateNew)
	assert.Equal(t, observedTask.Annotations.Name, "name2")
	assert.True(t, observedTask.NodeID == "id1")
}

func TestDeleteService(t *testing.T) {
	store := store.NewMemoryStore(nil)
	assert.NotNil(t, store)

	SetupCluster(t, store)

	watch, cancel := state.Watch(store.WatchQueue())
	defer cancel()

	skipEvents(t, watch)

	deleteService(t, store, service1)
	// task should be deleted
	observedTask := watchTaskDelete(t, watch)
	assert.Equal(t, observedTask.Annotations.Name, "name1")
	assert.Equal(t, observedTask.NodeID, "id1")
}

func TestRemoveTask(t *testing.T) {
	store := store.NewMemoryStore(nil)
	assert.NotNil(t, store)

	watch, cancel := state.Watch(store.WatchQueue() /*state.EventCreateTask{}, state.EventUpdateTask{}*/)
	defer cancel()

	SetupCluster(t, store)

	// get the task
	observedTask1 := watchTaskCreate(t, watch)
	assert.Equal(t, observedTask1.Status.State, api.TaskStateNew)
	assert.Equal(t, observedTask1.Annotations.Name, "name1")
	assert.Equal(t, observedTask1.NodeID, "id1")

	// delete the task
	deleteTask(t, store, observedTask1)

	// the task should be recreated
	observedTask2 := watchTaskCreate(t, watch)
	assert.Equal(t, observedTask2.Status.State, api.TaskStateNew)
	assert.Equal(t, observedTask2.Annotations.Name, "name1")
	assert.Equal(t, observedTask2.NodeID, "id1")
}

func addService(t *testing.T, store state.Store, service *api.Service) {
	store.Update(func(tx state.Tx) error {
		assert.NoError(t, tx.Services().Create(service))
		return nil
	})
}

func deleteService(t *testing.T, store state.Store, service *api.Service) {
	store.Update(func(tx state.Tx) error {
		assert.NoError(t, tx.Services().Delete(service.ID))
		return nil
	})
}

func addNode(t *testing.T, store state.Store, node *api.Node) {
	store.Update(func(tx state.Tx) error {
		assert.NoError(t, tx.Nodes().Create(node))
		return nil
	})
}

func deleteNode(t *testing.T, store state.Store, node *api.Node) {
	store.Update(func(tx state.Tx) error {
		assert.NoError(t, tx.Nodes().Delete(node.ID))
		return nil
	})
}

func addTask(t *testing.T, store state.Store, task *api.Task) {
	store.Update(func(tx state.Tx) error {
		assert.NoError(t, tx.Tasks().Create(task))
		return nil
	})
}

func deleteTask(t *testing.T, store state.Store, task *api.Task) {
	store.Update(func(tx state.Tx) error {
		assert.NoError(t, tx.Tasks().Delete(task.ID))
		return nil
	})
}
