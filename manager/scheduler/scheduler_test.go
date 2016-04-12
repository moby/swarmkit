package scheduler

import (
	"runtime"
	"strconv"
	"testing"
	"time"

	"github.com/docker/go-events"
	"github.com/docker/swarm-v2/identity"
	"github.com/docker/swarm-v2/manager/state"
	objectspb "github.com/docker/swarm-v2/pb/docker/cluster/objects"
	specspb "github.com/docker/swarm-v2/pb/docker/cluster/specs"
	typespb "github.com/docker/swarm-v2/pb/docker/cluster/types"
	"github.com/stretchr/testify/assert"
)

func TestScheduler(t *testing.T) {
	initialNodeSet := []*objectspb.Node{
		{
			ID: "id1",
			Spec: &specspb.NodeSpec{
				Meta: specspb.Meta{
					Name: "name1",
				},
			},
			Status: typespb.NodeStatus{
				State: typespb.NodeStatus_READY,
			},
		},
		{
			ID: "id2",
			Spec: &specspb.NodeSpec{
				Meta: specspb.Meta{
					Name: "name2",
				},
			},
			Status: typespb.NodeStatus{
				State: typespb.NodeStatus_READY,
			},
		},
		{
			ID: "id3",
			Spec: &specspb.NodeSpec{
				Meta: specspb.Meta{
					Name: "name2",
				},
			},
			Status: typespb.NodeStatus{
				State: typespb.NodeStatus_READY,
			},
		},
	}

	initialTaskSet := []*objectspb.Task{
		{
			ID:   "id1",
			Spec: &specspb.TaskSpec{},
			Meta: specspb.Meta{
				Name: "name1",
			},

			NodeID: initialNodeSet[0].ID,
		},
		{
			ID:   "id2",
			Spec: &specspb.TaskSpec{},
			Meta: specspb.Meta{
				Name: "name2",
			},
		},
		{
			ID:   "id3",
			Spec: &specspb.TaskSpec{},
			Meta: specspb.Meta{
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

	watch, cancel := state.Watch(store.WatchQueue(), state.EventUpdateTask{})
	defer cancel()

	go func() {
		assert.NoError(t, scheduler.Run())
	}()

	assignment1 := watchAssignment(t, watch)
	// must assign to id2 or id3 since id1 already has a task
	assert.Regexp(t, assignment1.NodeID, "(id2|id3)")

	assignment2 := watchAssignment(t, watch)
	// must assign to id2 or id3 since id1 already has a task
	if assignment1.NodeID == "id2" {
		assert.Equal(t, "id3", assignment2.NodeID)
	} else {
		assert.Equal(t, "id2", assignment2.NodeID)
	}

	err = store.Update(func(tx state.Tx) error {
		// Delete the task associated with node 1 so it's now the most lightly
		// loaded node.
		assert.NoError(t, tx.Tasks().Delete("id1"))

		// Create a new task. It should get assigned to id1.
		t4 := &objectspb.Task{
			ID:   "id4",
			Spec: &specspb.TaskSpec{},
			Meta: specspb.Meta{
				Name: "name4",
			},
		}
		assert.NoError(t, tx.Tasks().Create(t4))
		return nil
	})
	assert.NoError(t, err)

	assignment3 := watchAssignment(t, watch)
	assert.Equal(t, "id1", assignment3.NodeID)

	// Update a task to make it unassigned. It should get assigned by the
	// scheduler.
	err = store.Update(func(tx state.Tx) error {
		// Remove assignment from task id4. It should get assigned
		// to node id1.
		t4 := &objectspb.Task{
			ID:   "id4",
			Spec: &specspb.TaskSpec{},
			Meta: specspb.Meta{
				Name: "name4",
			},
		}
		assert.NoError(t, tx.Tasks().Update(t4))
		return nil
	})
	assert.NoError(t, err)

	assignment4 := watchAssignment(t, watch)
	assert.Equal(t, "id1", assignment4.NodeID)

	err = store.Update(func(tx state.Tx) error {
		// Create a ready node, then remove it. No tasks should ever
		// be assigned to it.
		node := &objectspb.Node{
			ID: "removednode",
			Spec: &specspb.NodeSpec{
				Meta: specspb.Meta{
					Name: "removednode",
				},
			},
			Status: typespb.NodeStatus{
				State: typespb.NodeStatus_DOWN,
			},
		}
		assert.NoError(t, tx.Nodes().Create(node))
		assert.NoError(t, tx.Nodes().Delete(node.ID))

		// Create an unassigned task.
		task := &objectspb.Task{
			ID:   "removednode",
			Spec: &specspb.TaskSpec{},
			Meta: specspb.Meta{
				Name: "removednode",
			},
		}
		assert.NoError(t, tx.Tasks().Create(task))
		return nil
	})
	assert.NoError(t, err)

	assignmentRemovedNode := watchAssignment(t, watch)
	assert.NotEqual(t, "removednode", assignmentRemovedNode.NodeID)

	err = store.Update(func(tx state.Tx) error {
		// Create a ready node. It should be used for the next
		// assignment.
		n4 := &objectspb.Node{
			ID: "id4",
			Spec: &specspb.NodeSpec{
				Meta: specspb.Meta{
					Name: "name4",
				},
			},
			Status: typespb.NodeStatus{
				State: typespb.NodeStatus_READY,
			},
		}
		assert.NoError(t, tx.Nodes().Create(n4))

		// Create an unassigned task.
		t5 := &objectspb.Task{
			ID:   "id5",
			Spec: &specspb.TaskSpec{},
			Meta: specspb.Meta{
				Name: "name5",
			},
		}
		assert.NoError(t, tx.Tasks().Create(t5))
		return nil
	})
	assert.NoError(t, err)

	assignment5 := watchAssignment(t, watch)
	assert.Equal(t, "id4", assignment5.NodeID)

	err = store.Update(func(tx state.Tx) error {
		// Create a non-ready node. It should NOT be used for the next
		// assignment.
		n5 := &objectspb.Node{
			ID: "id5",
			Spec: &specspb.NodeSpec{
				Meta: specspb.Meta{
					Name: "name5",
				},
			},
			Status: typespb.NodeStatus{
				State: typespb.NodeStatus_DOWN,
			},
		}
		assert.NoError(t, tx.Nodes().Create(n5))

		// Create an unassigned task.
		t6 := &objectspb.Task{
			ID:   "id6",
			Spec: &specspb.TaskSpec{},
			Meta: specspb.Meta{
				Name: "name6",
			},
		}
		assert.NoError(t, tx.Tasks().Create(t6))
		return nil
	})
	assert.NoError(t, err)

	assignment6 := watchAssignment(t, watch)
	assert.NotEqual(t, "id5", assignment6.NodeID)

	err = store.Update(func(tx state.Tx) error {
		// Update node id5 to put it in the READY state.
		n5 := &objectspb.Node{
			ID: "id5",
			Spec: &specspb.NodeSpec{
				Meta: specspb.Meta{
					Name: "name5",
				},
			},
			Status: typespb.NodeStatus{
				State: typespb.NodeStatus_READY,
			},
		}
		assert.NoError(t, tx.Nodes().Update(n5))

		// Create an unassigned task. Should be assigned to the
		// now-ready node.
		t7 := &objectspb.Task{
			ID:   "id7",
			Spec: &specspb.TaskSpec{},
			Meta: specspb.Meta{
				Name: "name7",
			},
		}
		assert.NoError(t, tx.Tasks().Create(t7))
		return nil
	})
	assert.NoError(t, err)

	assignment7 := watchAssignment(t, watch)
	assert.Equal(t, "id5", assignment7.NodeID)

	err = store.Update(func(tx state.Tx) error {
		// Create a ready node, then immediately take it down. The next
		// unassigned task should NOT be assigned to it.
		n6 := &objectspb.Node{
			ID: "id6",
			Spec: &specspb.NodeSpec{
				Meta: specspb.Meta{
					Name: "name6",
				},
			},
			Status: typespb.NodeStatus{
				State: typespb.NodeStatus_READY,
			},
		}
		assert.NoError(t, tx.Nodes().Create(n6))
		n6.Status.State = typespb.NodeStatus_DOWN
		assert.NoError(t, tx.Nodes().Update(n6))

		// Create an unassigned task.
		t8 := &objectspb.Task{
			ID:   "id8",
			Spec: &specspb.TaskSpec{},
			Meta: specspb.Meta{
				Name: "name8",
			},
		}
		assert.NoError(t, tx.Tasks().Create(t8))
		return nil
	})
	assert.NoError(t, err)

	assignment8 := watchAssignment(t, watch)
	assert.NotEqual(t, "id6", assignment8.NodeID)

	scheduler.Stop()
}

func TestSchedulerNoReadyNodes(t *testing.T) {
	initialTask := &objectspb.Task{
		ID:   "id1",
		Spec: &specspb.TaskSpec{},
		Meta: specspb.Meta{
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

	watch, cancel := state.Watch(store.WatchQueue(), state.EventUpdateTask{})
	defer cancel()

	go func() {
		assert.NoError(t, scheduler.Run())
	}()

	err = store.Update(func(tx state.Tx) error {
		// Create a ready node. The task should get assigned to this
		// node.
		node := &objectspb.Node{
			ID: "newnode",
			Spec: &specspb.NodeSpec{
				Meta: specspb.Meta{
					Name: "newnode",
				},
			},
			Status: typespb.NodeStatus{
				State: typespb.NodeStatus_READY,
			},
		}
		assert.NoError(t, tx.Nodes().Create(node))
		return nil
	})
	assert.NoError(t, err)

	assignment := watchAssignment(t, watch)
	assert.Equal(t, "newnode", assignment.NodeID)

	scheduler.Stop()
}

func watchAssignment(t *testing.T, watch chan events.Event) *objectspb.Task {
	for {
		select {
		case event := <-watch:
			if task, ok := event.(state.EventUpdateTask); ok {
				if task.Task.NodeID != "" {
					return task.Task
				}
			}
		case <-time.After(time.Second):
			t.Fatalf("no task assignment")
		}
	}
}

func BenchmarkScheduler1kNodes1kTasks(b *testing.B) {
	benchScheduler(b, 1e3, 1e3, false)
}

func BenchmarkScheduler1kNodes10kTasks(b *testing.B) {
	benchScheduler(b, 1e3, 1e4, false)
}

func BenchmarkScheduler1kNodes100kTasks(b *testing.B) {
	benchScheduler(b, 1e3, 1e5, false)
}

func BenchmarkScheduler100kNodes100kTasks(b *testing.B) {
	benchScheduler(b, 1e5, 1e5, false)
}

func BenchmarkScheduler100kNodes1MTasks(b *testing.B) {
	benchScheduler(b, 1e5, 1e6, false)
}

func BenchmarkSchedulerWorstCase1kNodes1kTasks(b *testing.B) {
	benchScheduler(b, 1e3, 1e3, true)
}

func BenchmarkSchedulerWorstCase1kNodes10kTasks(b *testing.B) {
	benchScheduler(b, 1e3, 1e4, true)
}

func BenchmarkSchedulerWorstCase1kNodes100kTasks(b *testing.B) {
	benchScheduler(b, 1e3, 1e5, true)
}

func BenchmarkSchedulerWorstCase100kNodes100kTasks(b *testing.B) {
	benchScheduler(b, 1e5, 1e5, true)
}

func BenchmarkSchedulerWorstCase100kNodes1MTasks(b *testing.B) {
	benchScheduler(b, 1e5, 1e6, true)
}

func benchScheduler(b *testing.B, nodes, tasks int, worstCase bool) {
	for iters := 0; iters < b.N; iters++ {
		b.StopTimer()
		s := state.NewMemoryStore(nil)
		scheduler := New(s)
		scheduler.scanAllNodes = worstCase

		watch, cancel := state.Watch(s.WatchQueue(), state.EventUpdateTask{})

		go func() {
			_ = scheduler.Run()
		}()

		// Let the scheduler get started
		runtime.Gosched()

		_ = s.Update(func(tx state.Tx) error {
			// Create initial nodes and tasks
			for i := 0; i < nodes; i++ {
				err := tx.Nodes().Create(&objectspb.Node{
					ID: identity.NewID(),
					Spec: &specspb.NodeSpec{
						Meta: specspb.Meta{
							Name: "name" + strconv.Itoa(i),
						},
					},
					Status: typespb.NodeStatus{
						State: typespb.NodeStatus_READY,
					},
				})
				if err != nil {
					panic(err)
				}
			}
			for i := 0; i < tasks; i++ {
				id := "task" + strconv.Itoa(i)
				err := tx.Tasks().Create(&objectspb.Task{
					ID:   id,
					Spec: &specspb.TaskSpec{},
					Meta: specspb.Meta{
						Name: id,
					},
				})
				if err != nil {
					panic(err)
				}
			}
			b.StartTimer()
			return nil
		})

		for i := 0; i != tasks; i++ {
			<-watch
		}

		scheduler.Stop()
		cancel()
	}
}
