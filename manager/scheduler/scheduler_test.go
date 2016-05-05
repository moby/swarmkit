package scheduler

import (
	"runtime"
	"strconv"
	"testing"
	"time"

	"github.com/docker/go-events"
	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/identity"
	"github.com/docker/swarm-v2/manager/state"
	"github.com/docker/swarm-v2/manager/state/store"
	"github.com/stretchr/testify/assert"
	"golang.org/x/net/context"
)

func TestScheduler(t *testing.T) {
	ctx := context.Background()
	initialNodeSet := []*api.Node{
		{
			ID: "id1",
			Spec: api.NodeSpec{
				Annotations: api.Annotations{
					Name: "name1",
				},
			},
			Status: api.NodeStatus{
				State: api.NodeStatus_READY,
			},
		},
		{
			ID: "id2",
			Spec: api.NodeSpec{
				Annotations: api.Annotations{
					Name: "name2",
				},
			},
			Status: api.NodeStatus{
				State: api.NodeStatus_READY,
			},
		},
		{
			ID: "id3",
			Spec: api.NodeSpec{
				Annotations: api.Annotations{
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
			ID: "id1",
			Annotations: api.Annotations{
				Name: "name1",
			},

			Status: api.TaskStatus{
				State: api.TaskStateAssigned,
			},
			NodeID: initialNodeSet[0].ID,
		},
		{
			ID: "id2",
			Annotations: api.Annotations{
				Name: "name2",
			},
			Status: api.TaskStatus{
				State: api.TaskStateAllocated,
			},
		},
		{
			ID: "id3",
			Annotations: api.Annotations{
				Name: "name2",
			},
			Status: api.TaskStatus{
				State: api.TaskStateAllocated,
			},
		},
	}

	store := store.NewMemoryStore(nil)
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
		assert.NoError(t, scheduler.Run(ctx))
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
		// Update each node to make sure this doesn't mess up the
		// scheduler's state.
		for _, n := range initialNodeSet {
			assert.NoError(t, tx.Nodes().Update(n))
		}
		return nil
	})
	assert.NoError(t, err)

	err = store.Update(func(tx state.Tx) error {
		// Delete the task associated with node 1 so it's now the most lightly
		// loaded node.
		assert.NoError(t, tx.Tasks().Delete("id1"))

		// Create a new task. It should get assigned to id1.
		t4 := &api.Task{
			ID: "id4",
			Annotations: api.Annotations{
				Name: "name4",
			},
			Status: api.TaskStatus{
				State: api.TaskStateAllocated,
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
		t4 := &api.Task{
			ID: "id4",
			Annotations: api.Annotations{
				Name: "name4",
			},
			Status: api.TaskStatus{
				State: api.TaskStateAllocated,
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
		node := &api.Node{
			ID: "removednode",
			Spec: api.NodeSpec{
				Annotations: api.Annotations{
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
			ID: "removednode",
			Annotations: api.Annotations{
				Name: "removednode",
			},
			Status: api.TaskStatus{
				State: api.TaskStateAllocated,
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
		n4 := &api.Node{
			ID: "id4",
			Spec: api.NodeSpec{
				Annotations: api.Annotations{
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
			ID: "id5",
			Annotations: api.Annotations{
				Name: "name5",
			},
			Status: api.TaskStatus{
				State: api.TaskStateAllocated,
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
		n5 := &api.Node{
			ID: "id5",
			Spec: api.NodeSpec{
				Annotations: api.Annotations{
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
			ID: "id6",
			Annotations: api.Annotations{
				Name: "name6",
			},
			Status: api.TaskStatus{
				State: api.TaskStateAllocated,
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
		n5 := &api.Node{
			ID: "id5",
			Spec: api.NodeSpec{
				Annotations: api.Annotations{
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
			ID: "id7",
			Annotations: api.Annotations{
				Name: "name7",
			},
			Status: api.TaskStatus{
				State: api.TaskStateAllocated,
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
		n6 := &api.Node{
			ID: "id6",
			Spec: api.NodeSpec{
				Annotations: api.Annotations{
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
			ID: "id8",
			Annotations: api.Annotations{
				Name: "name8",
			},
			Status: api.TaskStatus{
				State: api.TaskStateAllocated,
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
	ctx := context.Background()
	initialTask := &api.Task{
		ID: "id1",
		Annotations: api.Annotations{
			Name: "name1",
		},
		Status: api.TaskStatus{
			State: api.TaskStateAllocated,
		},
	}

	store := store.NewMemoryStore(nil)
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
		assert.NoError(t, scheduler.Run(ctx))
	}()

	err = store.Update(func(tx state.Tx) error {
		// Create a ready node. The task should get assigned to this
		// node.
		node := &api.Node{
			ID: "newnode",
			Spec: api.NodeSpec{
				Annotations: api.Annotations{
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
	assert.Equal(t, "newnode", assignment.NodeID)

	scheduler.Stop()
}

func TestSchedulerResourceConstraint(t *testing.T) {
	ctx := context.Background()
	// Create a ready node without enough memory to run the task.
	underprovisionedNode := &api.Node{
		ID: "underprovisioned",
		Spec: api.NodeSpec{
			Annotations: api.Annotations{
				Name: "underprovisioned",
			},
		},
		Status: api.NodeStatus{
			State: api.NodeStatus_READY,
		},
		Description: &api.NodeDescription{
			Resources: &api.Resources{
				NanoCPUs:    1e9,
				MemoryBytes: 1e9,
			},
		},
	}

	initialTask := &api.Task{
		ID: "id1",
		Spec: api.TaskSpec{
			Runtime: &api.TaskSpec_Container{
				Container: &api.Container{
					Resources: &api.ResourceRequirements{
						Reservations: &api.Resources{
							MemoryBytes: 2e9,
						},
					},
				},
			},
		},
		Annotations: api.Annotations{
			Name: "name1",
		},
		Status: api.TaskStatus{
			State: api.TaskStateAllocated,
		},
	}

	store := store.NewMemoryStore(nil)
	assert.NotNil(t, store)

	err := store.Update(func(tx state.Tx) error {
		// Add initial node and task
		assert.NoError(t, tx.Tasks().Create(initialTask))
		assert.NoError(t, tx.Nodes().Create(underprovisionedNode))
		return nil
	})
	assert.NoError(t, err)

	scheduler := New(store)

	watch, cancel := state.Watch(store.WatchQueue(), state.EventUpdateTask{})
	defer cancel()

	go func() {
		assert.NoError(t, scheduler.Run(ctx))
	}()

	err = store.Update(func(tx state.Tx) error {
		// Create a node with enough memory. The task should get
		// assigned to this node.
		node := &api.Node{
			ID: "bignode",
			Spec: api.NodeSpec{
				Annotations: api.Annotations{
					Name: "bignode",
				},
			},
			Description: &api.NodeDescription{
				Resources: &api.Resources{
					NanoCPUs:    4e9,
					MemoryBytes: 8e9,
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
	assert.Equal(t, "bignode", assignment.NodeID)

	scheduler.Stop()
}

func TestSchedulerResourceConstraintDeadTask(t *testing.T) {
	ctx := context.Background()
	// Create a ready node without enough memory to run the task.
	node := &api.Node{
		ID: "id1",
		Spec: api.NodeSpec{
			Annotations: api.Annotations{
				Name: "node",
			},
		},
		Status: api.NodeStatus{
			State: api.NodeStatus_READY,
		},
		Description: &api.NodeDescription{
			Resources: &api.Resources{
				NanoCPUs:    1e9,
				MemoryBytes: 1e9,
			},
		},
	}

	bigTask1 := &api.Task{
		ID: "id1",
		Spec: api.TaskSpec{
			Runtime: &api.TaskSpec_Container{
				Container: &api.Container{
					Resources: &api.ResourceRequirements{
						Reservations: &api.Resources{
							MemoryBytes: 8e8,
						},
					},
				},
			},
		},
		Annotations: api.Annotations{
			Name: "big",
		},
		Status: api.TaskStatus{
			State: api.TaskStateAllocated,
		},
	}

	bigTask2 := bigTask1.Copy()
	bigTask2.ID = "id2"

	store := store.NewMemoryStore(nil)
	assert.NotNil(t, store)

	err := store.Update(func(tx state.Tx) error {
		// Add initial node and task
		assert.NoError(t, tx.Nodes().Create(node))
		assert.NoError(t, tx.Tasks().Create(bigTask1))
		return nil
	})
	assert.NoError(t, err)

	scheduler := New(store)

	watch, cancel := state.Watch(store.WatchQueue(), state.EventUpdateTask{})
	defer cancel()

	go func() {
		assert.NoError(t, scheduler.Run(ctx))
	}()

	// The task fits, so it should get assigned
	assignment := watchAssignment(t, watch)
	assert.Equal(t, "id1", assignment.ID)
	assert.Equal(t, "id1", assignment.NodeID)

	err = store.Update(func(tx state.Tx) error {
		// Add a second task. It shouldn't get assigned because of
		// resource constraints.
		return tx.Tasks().Create(bigTask2)
	})
	assert.NoError(t, err)

	time.Sleep(100 * time.Millisecond)
	store.View(func(tx state.ReadTx) {
		tasks, err := tx.Tasks().Find(state.ByNodeID(node.ID))
		assert.NoError(t, err)
		assert.Len(t, tasks, 1)
	})

	err = store.Update(func(tx state.Tx) error {
		// The task becomes dead
		updatedTask := tx.Tasks().Get(bigTask1.ID)
		updatedTask.Status.State = api.TaskStateDead
		return tx.Tasks().Update(updatedTask)
	})
	assert.NoError(t, err)

	// With the first task no longer consuming resources, the second
	// one can be scheduled.
	assignment = watchAssignment(t, watch)
	assert.Equal(t, "id1", assignment.ID)
	assert.Equal(t, "id1", assignment.NodeID)

	scheduler.Stop()
}

func TestPreassignedTasks(t *testing.T) {
	ctx := context.Background()
	initialNodeSet := []*api.Node{
		{
			ID: "node1",
			Spec: api.NodeSpec{
				Annotations: api.Annotations{
					Name: "name1",
				},
			},
			Status: api.NodeStatus{
				State: api.NodeStatus_READY,
			},
		},
		{
			ID: "node2",
			Spec: api.NodeSpec{
				Annotations: api.Annotations{
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
			ID: "task1",
			Annotations: api.Annotations{
				Name: "name1",
			},

			Status: api.TaskStatus{
				State: api.TaskStateAllocated,
			},
		},
		{
			ID: "task2",
			Annotations: api.Annotations{
				Name: "name2",
			},
			Status: api.TaskStatus{
				State: api.TaskStateAllocated,
			},
			NodeID: initialNodeSet[0].ID,
		},
		{
			ID: "task3",
			Annotations: api.Annotations{
				Name: "name2",
			},
			Status: api.TaskStatus{
				State: api.TaskStateAllocated,
			},
			NodeID: initialNodeSet[0].ID,
		},
	}

	store := store.NewMemoryStore(nil)
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
		assert.NoError(t, scheduler.Run(ctx))
	}()

	//preassigned tasks would be processed first
	assignment1 := watchAssignment(t, watch)
	// task2 and task3 are preassigned to node1
	assert.Equal(t, assignment1.NodeID, "node1")
	assert.Regexp(t, assignment1.ID, "(task2|task3)")

	assignment2 := watchAssignment(t, watch)
	if assignment1.ID == "task2" {
		assert.Equal(t, "task3", assignment2.ID)
	} else {
		assert.Equal(t, "task2", assignment2.ID)
	}

	// task1 would be assigned to node2 because node1 has 2 tasks already
	assignment3 := watchAssignment(t, watch)
	assert.Equal(t, assignment3.ID, "task1")
	assert.Equal(t, assignment3.NodeID, "node2")
}

func watchAssignment(t *testing.T, watch chan events.Event) *api.Task {
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
	ctx := context.Background()
	for iters := 0; iters < b.N; iters++ {
		b.StopTimer()
		s := store.NewMemoryStore(nil)
		scheduler := New(s)
		scheduler.scanAllNodes = worstCase

		watch, cancel := state.Watch(s.WatchQueue(), state.EventUpdateTask{})

		go func() {
			_ = scheduler.Run(ctx)
		}()

		// Let the scheduler get started
		runtime.Gosched()

		_ = s.Update(func(tx state.Tx) error {
			// Create initial nodes and tasks
			for i := 0; i < nodes; i++ {
				err := tx.Nodes().Create(&api.Node{
					ID: identity.NewID(),
					Spec: api.NodeSpec{
						Annotations: api.Annotations{
							Name: "name" + strconv.Itoa(i),
						},
					},
					Status: api.NodeStatus{
						State: api.NodeStatus_READY,
					},
				})
				if err != nil {
					panic(err)
				}
			}
			for i := 0; i < tasks; i++ {
				id := "task" + strconv.Itoa(i)
				err := tx.Tasks().Create(&api.Task{
					ID: id,
					Annotations: api.Annotations{
						Name: id,
					},
					Status: api.TaskStatus{
						State: api.TaskStateAllocated,
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
