package scheduler

import (
	"context"
	"testing"

	"github.com/moby/swarmkit/v2/api"
	"github.com/moby/swarmkit/v2/manager/state/store"
	"github.com/stretchr/testify/require"
)

func TestPendingOldRevisionRecoversWhenNodeReturns(t *testing.T) {
	ctx := context.Background()
	state := store.NewMemoryStore(nil)
	defer state.Close()
	service := &api.Service{ID: "service", SpecVersion: &api.Version{Index: 2}}
	task := &api.Task{
		ID: "pending", ServiceID: service.ID, SpecVersion: &api.Version{Index: 1},
		DesiredState: api.TaskStateRunning, Status: api.TaskStatus{State: api.TaskStatePending},
		Spec: api.TaskSpec{Runtime: &api.TaskSpec_Container{Container: &api.ContainerSpec{Image: "busybox:1.37"}}},
	}
	// A service-level metadata update changes the revision, not the task spec.
	service.Spec.Task = task.Spec
	node := &api.Node{
		ID: "worker", Spec: api.NodeSpec{Availability: api.NodeAvailabilityActive},
		Status: api.NodeStatus{State: api.NodeStatus_DOWN}, Description: &api.NodeDescription{},
	}
	require.NoError(t, state.Update(func(tx store.Tx) error {
		require.NoError(t, store.CreateService(tx, service))
		require.NoError(t, store.CreateTask(tx, task))
		return store.CreateNode(tx, node)
	}))
	scheduler := New(state)
	state.View(func(tx store.ReadTx) { require.NoError(t, scheduler.setupTasksList(tx)) })
	scheduler.tick(ctx)
	require.Contains(t, scheduler.unassignedTasks, task.ID,
		"an older spec revision must not discard a task that is still desired running")
	node.Status.State = api.NodeStatus_READY
	require.NoError(t, state.Update(func(tx store.Tx) error { return store.UpdateNode(tx, node) }))
	scheduler.createOrUpdateNode(node)
	scheduler.tick(ctx)
	state.View(func(tx store.ReadTx) {
		recovered := store.GetTask(tx, task.ID)
		require.Equal(t, api.TaskStateAssigned, recovered.Status.State)
		require.Equal(t, node.ID, recovered.NodeID)
	})
}
