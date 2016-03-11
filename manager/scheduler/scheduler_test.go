package scheduler

import (
	"testing"
	"time"

	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/manager/state"
	"github.com/docker/swarm-v2/manager/state/watch"
	"github.com/stretchr/testify/assert"
)

func TestScheduler(t *testing.T) {
	initialNodeSet := []*api.Node{
		{
			ID: "id1",
			Spec: &api.NodeSpec{
				Meta: api.Meta{
					Name: "name1",
				},
			},
			Status: api.NodeStatus{
				State: api.NodeStatus_READY,
			},
		},
		{
			ID: "id2",
			Spec: &api.NodeSpec{
				Meta: api.Meta{
					Name: "name2",
				},
			},
			Status: api.NodeStatus{
				State: api.NodeStatus_READY,
			},
		},
		{
			ID: "id3",
			Spec: &api.NodeSpec{
				Meta: api.Meta{
					Name: "name2",
				},
			},
			Status: api.NodeStatus{
				State: api.NodeStatus_READY,
			},
		},
	}

	initialTaskSet := []*api.Task{
		{
			ID:   "id1",
			Spec: &api.TaskSpec{},
			Meta: api.Meta{
				Name: "name1",
			},

			NodeID: initialNodeSet[0].ID,
		},
		{
			ID:   "id2",
			Spec: &api.TaskSpec{},
			Meta: api.Meta{
				Name: "name2",
			},
		},
		{
			ID:   "id3",
			Spec: &api.TaskSpec{},
			Meta: api.Meta{
				Name: "name2",
			},
		},
	}

	store := state.NewMemoryStore(nil)
	assert.NotNil(t, store)

	err := store.Update(func(tx state.Tx) error {
		// Prepoulate nodes
		for _, n := range initialNodeSet {
			assert.NoError(t, tx.Nodes().Create(n))
		}

		// Prepopulate tasks
		for _, task := range initialTaskSet {
			assert.NoError(t, tx.Tasks().Create(task))
		}
		return nil
	})
	assert.NoError(t, err)

	scheduler := New(store)

	watch := state.Watch(store.WatchQueue(), state.EventUpdateTask{})
	defer store.WatchQueue().StopWatch(watch)

	go func() {
		assert.NoError(t, scheduler.Run())
	}()

	assignment1 := watchAssignment(t, watch)
	// must assign to id2 or id3 since id1 already has a task
	assert.Regexp(t, assignment1.NodeID, "(id2|id3)")

	assignment2 := watchAssignment(t, watch)
	// must assign to id2 or id3 since id1 already has a task
	if assignment1.NodeID == "id2" {
		assert.Equal(t, assignment2.NodeID, "id3")
	} else {
		assert.Equal(t, assignment2.NodeID, "id2")
	}

	err = store.Update(func(tx state.Tx) error {
		// Delete the task associated with node 1 so it's now the most lightly
		// loaded node.
		assert.NoError(t, tx.Tasks().Delete("id1"))

		// Create a new task. It should get assigned to id1.
		t4 := &api.Task{
			ID:   "id4",
			Spec: &api.TaskSpec{},
			Meta: api.Meta{
				Name: "name4",
			},
		}
		assert.NoError(t, tx.Tasks().Create(t4))
		return nil
	})
	assert.NoError(t, err)

	assignment3 := watchAssignment(t, watch)
	assert.Equal(t, assignment3.NodeID, "id1")

	// Update a task to make it unassigned. It should get assigned by the
	// scheduler.
	err = store.Update(func(tx state.Tx) error {
		// Remove assignment from task id4. It should get assigned
		// to node id1.
		t4 := &api.Task{
			ID:   "id4",
			Spec: &api.TaskSpec{},
			Meta: api.Meta{
				Name: "name4",
			},
		}
		assert.NoError(t, tx.Tasks().Update(t4))
		return nil
	})
	assert.NoError(t, err)

	// Discard first UpdateTask - that's our own UpdateTask
	watchAssignment(t, watch)
	assignment4 := watchAssignment(t, watch)
	assert.Equal(t, assignment4.NodeID, "id1")

	err = store.Update(func(tx state.Tx) error {
		// Create a ready node, then remove it. No tasks should ever
		// be assigned to it.
		node := &api.Node{
			ID: "removednode",
			Spec: &api.NodeSpec{
				Meta: api.Meta{
					Name: "removednode",
				},
			},
			Status: api.NodeStatus{
				State: api.NodeStatus_DOWN,
			},
		}
		assert.NoError(t, tx.Nodes().Create(node))
		assert.NoError(t, tx.Nodes().Delete(node.ID))

		// Create an unassigned task.
		task := &api.Task{
			ID:   "removednode",
			Spec: &api.TaskSpec{},
			Meta: api.Meta{
				Name: "removednode",
			},
		}
		assert.NoError(t, tx.Tasks().Create(task))
		return nil
	})
	assert.NoError(t, err)

	assignmentRemovedNode := watchAssignment(t, watch)
	assert.NotEqual(t, assignmentRemovedNode.NodeID, "removednode")

	err = store.Update(func(tx state.Tx) error {
		// Create a ready node. It should be used for the next
		// assignment.
		n4 := &api.Node{
			ID: "id4",
			Spec: &api.NodeSpec{
				Meta: api.Meta{
					Name: "name4",
				},
			},
			Status: api.NodeStatus{
				State: api.NodeStatus_READY,
			},
		}
		assert.NoError(t, tx.Nodes().Create(n4))

		// Create an unassigned task.
		t5 := &api.Task{
			ID:   "id5",
			Spec: &api.TaskSpec{},
			Meta: api.Meta{
				Name: "name5",
			},
		}
		assert.NoError(t, tx.Tasks().Create(t5))
		return nil
	})
	assert.NoError(t, err)

	assignment5 := watchAssignment(t, watch)
	assert.Equal(t, assignment5.NodeID, "id4")

	err = store.Update(func(tx state.Tx) error {
		// Create a non-ready node. It should NOT be used for the next
		// assignment.
		n5 := &api.Node{
			ID: "id5",
			Spec: &api.NodeSpec{
				Meta: api.Meta{
					Name: "name5",
				},
			},
			Status: api.NodeStatus{
				State: api.NodeStatus_DOWN,
			},
		}
		assert.NoError(t, tx.Nodes().Create(n5))

		// Create an unassigned task.
		t6 := &api.Task{
			ID:   "id6",
			Spec: &api.TaskSpec{},
			Meta: api.Meta{
				Name: "name6",
			},
		}
		assert.NoError(t, tx.Tasks().Create(t6))
		return nil
	})
	assert.NoError(t, err)

	assignment6 := watchAssignment(t, watch)
	assert.NotEqual(t, assignment6.NodeID, "id5")

	err = store.Update(func(tx state.Tx) error {
		// Update node id5 to put it in the READY state.
		n5 := &api.Node{
			ID: "id5",
			Spec: &api.NodeSpec{
				Meta: api.Meta{
					Name: "name5",
				},
			},
			Status: api.NodeStatus{
				State: api.NodeStatus_READY,
			},
		}
		assert.NoError(t, tx.Nodes().Update(n5))

		// Create an unassigned task. Should be assigned to the
		// now-ready node.
		t7 := &api.Task{
			ID:   "id7",
			Spec: &api.TaskSpec{},
			Meta: api.Meta{
				Name: "name7",
			},
		}
		assert.NoError(t, tx.Tasks().Create(t7))
		return nil
	})
	assert.NoError(t, err)

	assignment7 := watchAssignment(t, watch)
	assert.Equal(t, assignment7.NodeID, "id5")

	err = store.Update(func(tx state.Tx) error {
		// Create a ready node, then immediately take it down. The next
		// unassigned task should NOT be assigned to it.
		n6 := &api.Node{
			ID: "id6",
			Spec: &api.NodeSpec{
				Meta: api.Meta{
					Name: "name6",
				},
			},
			Status: api.NodeStatus{
				State: api.NodeStatus_READY,
			},
		}
		assert.NoError(t, tx.Nodes().Create(n6))
		n6.Status.State = api.NodeStatus_DOWN
		assert.NoError(t, tx.Nodes().Update(n6))

		// Create an unassigned task.
		t8 := &api.Task{
			ID:   "id8",
			Spec: &api.TaskSpec{},
			Meta: api.Meta{
				Name: "name8",
			},
		}
		assert.NoError(t, tx.Tasks().Create(t8))
		return nil
	})
	assert.NoError(t, err)

	assignment8 := watchAssignment(t, watch)
	assert.NotEqual(t, assignment8.NodeID, "id6")

	scheduler.Stop()
}

func TestSchedulerNoReadyNodes(t *testing.T) {
	initialTask := &api.Task{
		ID:   "id1",
		Spec: &api.TaskSpec{},
		Meta: api.Meta{
			Name: "name1",
		},
	}

	store := state.NewMemoryStore(nil)
	assert.NotNil(t, store)

	err := store.Update(func(tx state.Tx) error {
		// Add initial task
		assert.NoError(t, tx.Tasks().Create(initialTask))
		return nil
	})
	assert.NoError(t, err)

	scheduler := New(store)

	watch := state.Watch(store.WatchQueue(), state.EventUpdateTask{})
	defer store.WatchQueue().StopWatch(watch)

	go func() {
		assert.NoError(t, scheduler.Run())
	}()

	err = store.Update(func(tx state.Tx) error {
		// Create a ready node. The task should get assigned to this
		// node.
		node := &api.Node{
			ID: "newnode",
			Spec: &api.NodeSpec{
				Meta: api.Meta{
					Name: "newnode",
				},
			},
			Status: api.NodeStatus{
				State: api.NodeStatus_READY,
			},
		}
		assert.NoError(t, tx.Nodes().Create(node))
		return nil
	})
	assert.NoError(t, err)

	assignment := watchAssignment(t, watch)
	assert.Equal(t, assignment.NodeID, "newnode")

	scheduler.Stop()
}

func watchAssignment(t *testing.T, watch chan watch.Event) *api.Task {
	for {
		select {
		case event := <-watch:
			if task, ok := event.Payload.(state.EventUpdateTask); ok {
				return task.Task
			}
		case <-time.After(time.Second):
			t.Fatalf("no task assignment")
		}
	}
}
