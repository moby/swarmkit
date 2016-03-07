package state

import (
	"errors"
	"strconv"
	"sync"
	"testing"

	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/identity"
	"github.com/stretchr/testify/assert"
)

func TestStoreNode(t *testing.T) {
	nodeSet := []*api.Node{
		{
			ID: "id1",
			Spec: &api.NodeSpec{
				Meta: &api.Meta{
					Name: "name1",
				},
			},
		},
		{
			ID: "id2",
			Spec: &api.NodeSpec{
				Meta: &api.Meta{
					Name: "name2",
				},
			},
		},
		{
			ID: "id3",
			Spec: &api.NodeSpec{
				Meta: &api.Meta{
					Name: "name2",
				},
			},
		},
	}

	s := NewMemoryStore()
	assert.NotNil(t, s)

	err := s.View(func(readTx ReadTx) error {
		allNodes, err := readTx.Nodes().Find(All)
		assert.NoError(t, err)
		assert.Empty(t, allNodes)
		return nil
	})
	assert.NoError(t, err)

	err = s.Update(func(tx Tx) error {
		for _, n := range nodeSet {
			assert.NoError(t, tx.Nodes().Create(n))
		}
		allNodes, err := tx.Nodes().Find(All)
		assert.NoError(t, err)
		assert.Len(t, allNodes, len(nodeSet))

		assert.Error(t, tx.Nodes().Create(nodeSet[0]), "duplicate IDs must be rejected")
		return nil
	})
	assert.NoError(t, err)

	err = s.View(func(readTx ReadTx) error {
		assert.Equal(t, nodeSet[0], readTx.Nodes().Get("id1"))
		assert.Equal(t, nodeSet[1], readTx.Nodes().Get("id2"))
		assert.Equal(t, nodeSet[2], readTx.Nodes().Get("id3"))

		foundNodes, err := readTx.Nodes().Find(ByName("name1"))
		assert.NoError(t, err)
		assert.Len(t, foundNodes, 1)
		foundNodes, err = readTx.Nodes().Find(ByName("name2"))
		assert.NoError(t, err)
		assert.Len(t, foundNodes, 2)
		foundNodes, err = readTx.Nodes().Find(ByName("invalid"))
		assert.NoError(t, err)
		assert.Len(t, foundNodes, 0)
		return nil
	})
	assert.NoError(t, err)

	// Update.
	update := &api.Node{
		ID: "id3",
		Spec: &api.NodeSpec{
			Meta: &api.Meta{
				Name: "name3",
			},
		},
	}
	err = s.Update(func(tx Tx) error {
		assert.NotEqual(t, update, tx.Nodes().Get("id3"))
		assert.NoError(t, tx.Nodes().Update(update))
		assert.Equal(t, update, tx.Nodes().Get("id3"))

		foundNodes, err := tx.Nodes().Find(ByName("name2"))
		assert.NoError(t, err)
		assert.Len(t, foundNodes, 1)
		foundNodes, err = tx.Nodes().Find(ByName("name3"))
		assert.NoError(t, err)
		assert.Len(t, foundNodes, 1)

		invalidUpdate := *nodeSet[0]
		invalidUpdate.ID = "invalid"
		assert.Error(t, tx.Nodes().Update(&invalidUpdate), "invalid IDs should be rejected")

		// Delete
		assert.NotNil(t, tx.Nodes().Get("id1"))
		assert.NoError(t, tx.Nodes().Delete("id1"))
		assert.Nil(t, tx.Nodes().Get("id1"))
		foundNodes, err = tx.Nodes().Find(ByName("name1"))
		assert.NoError(t, err)
		assert.Empty(t, foundNodes)

		assert.Equal(t, tx.Nodes().Delete("nonexistent"), ErrNotExist)
		return nil
	})
	assert.NoError(t, err)
}

func TestStoreJob(t *testing.T) {
	jobSet := []*api.Job{
		{
			ID: "id1",
			Spec: &api.JobSpec{
				Meta: &api.Meta{
					Name: "name1",
				},
			},
		},
		{
			ID: "id2",
			Spec: &api.JobSpec{
				Meta: &api.Meta{
					Name: "name2",
				},
			},
		},
		{
			ID: "id3",
			Spec: &api.JobSpec{
				Meta: &api.Meta{
					// intentionally conflicting name
					Name: "name2",
				},
			},
		},
	}

	s := NewMemoryStore()
	assert.NotNil(t, s)

	err := s.View(func(readTx ReadTx) error {
		allJobs, err := readTx.Jobs().Find(All)
		assert.NoError(t, err)
		assert.Empty(t, allJobs)
		return nil
	})
	assert.NoError(t, err)

	err = s.Update(func(tx Tx) error {
		for _, j := range jobSet {
			assert.NoError(t, tx.Jobs().Create(j))
		}
		allJobs, err := tx.Jobs().Find(All)
		assert.NoError(t, err)
		assert.Len(t, allJobs, len(jobSet))

		assert.Error(t, tx.Jobs().Create(jobSet[0]), "duplicate IDs must be rejected")
		return nil
	})
	assert.NoError(t, err)

	err = s.View(func(readTx ReadTx) error {
		assert.Equal(t, jobSet[0], readTx.Jobs().Get("id1"))
		assert.Equal(t, jobSet[1], readTx.Jobs().Get("id2"))
		assert.Equal(t, jobSet[2], readTx.Jobs().Get("id3"))

		foundJobs, err := readTx.Jobs().Find(ByName("name1"))
		assert.NoError(t, err)
		assert.Len(t, foundJobs, 1)
		foundJobs, err = readTx.Jobs().Find(ByName("name2"))
		assert.NoError(t, err)
		assert.Len(t, foundJobs, 2)
		foundJobs, err = readTx.Jobs().Find(ByName("invalid"))
		assert.NoError(t, err)
		assert.Len(t, foundJobs, 0)
		return nil
	})
	assert.NoError(t, err)

	// Update.
	update := &api.Job{
		ID: "id3",
		Spec: &api.JobSpec{
			Meta: &api.Meta{
				// intentionally conflicting name
				Name: "name3",
			},
		},
	}
	err = s.Update(func(tx Tx) error {
		assert.NotEqual(t, update, tx.Jobs().Get("id3"))
		assert.NoError(t, tx.Jobs().Update(update))
		assert.Equal(t, update, tx.Jobs().Get("id3"))

		foundJobs, err := tx.Jobs().Find(ByName("name2"))
		assert.NoError(t, err)
		assert.Len(t, foundJobs, 1)
		foundJobs, err = tx.Jobs().Find(ByName("name3"))
		assert.NoError(t, err)
		assert.Len(t, foundJobs, 1)

		invalidUpdate := *jobSet[0]
		invalidUpdate.ID = "invalid"
		assert.Error(t, tx.Jobs().Update(&invalidUpdate), "invalid IDs should be rejected")

		// Delete
		assert.NotNil(t, tx.Jobs().Get("id1"))
		assert.NoError(t, tx.Jobs().Delete("id1"))
		assert.Nil(t, tx.Jobs().Get("id1"))
		foundJobs, err = tx.Jobs().Find(ByName("name1"))
		assert.NoError(t, err)
		assert.Empty(t, foundJobs)

		assert.Equal(t, tx.Jobs().Delete("nonexistent"), ErrNotExist)
		return nil
	})
	assert.NoError(t, err)
}

func TestStoreNetwork(t *testing.T) {
	networkSet := []*api.Network{
		{
			ID: "id1",
			Spec: &api.NetworkSpec{
				Meta: &api.Meta{
					Name: "name1",
				},
			},
		},
		{
			ID: "id2",
			Spec: &api.NetworkSpec{
				Meta: &api.Meta{
					Name: "name2",
				},
			},
		},
		{
			ID: "id3",
			Spec: &api.NetworkSpec{
				Meta: &api.Meta{
					// intentionally conflicting name
					Name: "name2",
				},
			},
		},
	}

	s := NewMemoryStore()
	assert.NotNil(t, s)

	err := s.View(func(readTx ReadTx) error {
		allNetworks, err := readTx.Networks().Find(All)
		assert.NoError(t, err)
		assert.Empty(t, allNetworks)
		return nil
	})
	assert.NoError(t, err)

	err = s.Update(func(tx Tx) error {
		for _, n := range networkSet {
			assert.NoError(t, tx.Networks().Create(n))
		}
		allNetworks, err := tx.Networks().Find(All)
		assert.NoError(t, err)
		assert.Len(t, allNetworks, len(networkSet))

		assert.Error(t, tx.Networks().Create(networkSet[0]), "duplicate IDs must be rejected")
		return nil
	})
	assert.NoError(t, err)

	err = s.View(func(readTx ReadTx) error {
		assert.Equal(t, networkSet[0], readTx.Networks().Get("id1"))
		assert.Equal(t, networkSet[1], readTx.Networks().Get("id2"))
		assert.Equal(t, networkSet[2], readTx.Networks().Get("id3"))

		foundNetworks, err := readTx.Networks().Find(ByName("name1"))
		assert.NoError(t, err)
		assert.Len(t, foundNetworks, 1)
		foundNetworks, err = readTx.Networks().Find(ByName("name2"))
		assert.NoError(t, err)
		assert.Len(t, foundNetworks, 2)
		foundNetworks, err = readTx.Networks().Find(ByName("invalid"))
		assert.NoError(t, err)
		assert.Len(t, foundNetworks, 0)
		return nil
	})
	assert.NoError(t, err)

	err = s.Update(func(tx Tx) error {
		// Delete
		assert.NotNil(t, tx.Networks().Get("id1"))
		assert.NoError(t, tx.Networks().Delete("id1"))
		assert.Nil(t, tx.Networks().Get("id1"))
		foundNetworks, err := tx.Networks().Find(ByName("name1"))
		assert.NoError(t, err)
		assert.Empty(t, foundNetworks)

		assert.Equal(t, tx.Tasks().Delete("nonexistent"), ErrNotExist)
		return nil
	})

	assert.NoError(t, err)
}

func TestStoreTask(t *testing.T) {
	node := &api.Node{
		ID: "node1",
		Spec: &api.NodeSpec{
			Meta: &api.Meta{
				Name: "node-name1",
			},
		},
	}
	job := &api.Job{
		ID: "job1",
		Spec: &api.JobSpec{
			Meta: &api.Meta{
				Name: "job-name1",
			},
		},
	}
	taskSet := []*api.Task{
		{
			ID: "id1",
			Spec: &api.JobSpec{
				Meta: &api.Meta{
					Name: "name1",
				},
			},
			NodeID: node.ID,
		},
		{
			ID: "id2",
			Spec: &api.JobSpec{
				Meta: &api.Meta{
					Name: "name2",
				},
			},
			JobID: job.ID,
		},
		{
			ID: "id3",
			Spec: &api.JobSpec{
				Meta: &api.Meta{
					Name: "name2",
				},
			},
		},
	}

	s := NewMemoryStore()
	assert.NotNil(t, s)

	err := s.Update(func(tx Tx) error {
		assert.NoError(t, tx.Nodes().Create(node))
		assert.NoError(t, tx.Jobs().Create(job))

		allTasks, err := tx.Tasks().Find(All)
		assert.NoError(t, err)
		assert.Empty(t, allTasks)

		for _, task := range taskSet {
			assert.NoError(t, tx.Tasks().Create(task))
		}

		allTasks, err = tx.Tasks().Find(All)
		assert.NoError(t, err)
		assert.Len(t, allTasks, len(taskSet))

		assert.Error(t, tx.Tasks().Create(taskSet[0]), "duplicate IDs must be rejected")
		return nil
	})
	assert.NoError(t, err)

	err = s.View(func(readTx ReadTx) error {
		assert.Equal(t, taskSet[0], readTx.Tasks().Get("id1"))
		assert.Equal(t, taskSet[1], readTx.Tasks().Get("id2"))
		assert.Equal(t, taskSet[2], readTx.Tasks().Get("id3"))

		foundTasks, err := readTx.Tasks().Find(ByName("name1"))
		assert.NoError(t, err)
		assert.Len(t, foundTasks, 1)
		foundTasks, err = readTx.Tasks().Find(ByName("name2"))
		assert.NoError(t, err)
		assert.Len(t, foundTasks, 2)
		foundTasks, err = readTx.Tasks().Find(ByName("invalid"))
		assert.NoError(t, err)
		assert.Len(t, foundTasks, 0)

		foundTasks, err = readTx.Tasks().Find(ByNodeID(node.ID))
		assert.NoError(t, err)
		assert.Len(t, foundTasks, 1)
		assert.Equal(t, foundTasks[0], taskSet[0])
		foundTasks, err = readTx.Tasks().Find(ByNodeID("invalid"))
		assert.NoError(t, err)
		assert.Len(t, foundTasks, 0)

		foundTasks, err = readTx.Tasks().Find(ByJobID(job.ID))
		assert.NoError(t, err)
		assert.Len(t, foundTasks, 1)
		assert.Equal(t, foundTasks[0], taskSet[1])
		foundTasks, err = readTx.Tasks().Find(ByJobID("invalid"))
		assert.NoError(t, err)
		assert.Len(t, foundTasks, 0)
		return nil
	})
	assert.NoError(t, err)

	// Update.
	update := &api.Task{
		ID: "id3",

		// NOTE(stevvooe): It doesn't entirely make sense to updating task to
		// have a different name on the job spec. We are mostly doing this to
		// test that the store works.
		Spec: &api.JobSpec{
			Meta: &api.Meta{
				Name: "name3",
			},
		},
	}
	err = s.Update(func(tx Tx) error {
		assert.NotEqual(t, update, tx.Tasks().Get("id3"))
		assert.NoError(t, tx.Tasks().Update(update))
		assert.Equal(t, update, tx.Tasks().Get("id3"))

		foundTasks, err := tx.Tasks().Find(ByName("name2"))
		assert.NoError(t, err)
		assert.Len(t, foundTasks, 1)
		foundTasks, err = tx.Tasks().Find(ByName("name3"))
		assert.NoError(t, err)
		assert.Len(t, foundTasks, 1)

		invalidUpdate := *taskSet[0]
		invalidUpdate.ID = "invalid"
		assert.Error(t, tx.Tasks().Update(&invalidUpdate), "invalid IDs should be rejected")

		// Delete
		assert.NotNil(t, tx.Tasks().Get("id1"))
		assert.NoError(t, tx.Tasks().Delete("id1"))
		assert.Nil(t, tx.Tasks().Get("id1"))
		foundTasks, err = tx.Tasks().Find(ByName("name1"))
		assert.NoError(t, err)
		assert.Empty(t, foundTasks)

		assert.Equal(t, tx.Tasks().Delete("nonexistent"), ErrNotExist)
		return nil
	})
	assert.NoError(t, err)
}

func TestStoreSnapshot(t *testing.T) {
	nodeSet := []*api.Node{
		{
			ID: "id1",
			Spec: &api.NodeSpec{
				Meta: &api.Meta{
					Name: "name1",
				},
			},
		},
		{
			ID: "id2",
			Spec: &api.NodeSpec{
				Meta: &api.Meta{
					Name: "name2",
				},
			},
		},
		{
			ID: "id3",
			Spec: &api.NodeSpec{
				Meta: &api.Meta{
					Name: "name2",
				},
			},
		},
	}

	jobSet := []*api.Job{
		{
			ID: "id1",
			Spec: &api.JobSpec{
				Meta: &api.Meta{
					Name: "name1",
				},
			},
		},
		{
			ID: "id2",
			Spec: &api.JobSpec{
				Meta: &api.Meta{
					Name: "name2",
				},
			},
		},
		{
			ID: "id3",
			Spec: &api.JobSpec{
				Meta: &api.Meta{
					Name: "name2",
				},
			},
		},
	}

	taskSet := []*api.Task{
		{
			ID: "id1",
			Spec: &api.JobSpec{
				Meta: &api.Meta{
					Name: "name1",
				},
			},
			NodeID: nodeSet[0].ID,
		},
		{
			ID: "id2",
			Spec: &api.JobSpec{
				Meta: &api.Meta{
					Name: "name2",
				},
			},
			JobID: jobSet[0].ID,
		},
		{
			ID: "id3",
			Spec: &api.JobSpec{
				Meta: &api.Meta{
					Name: "name2",
				},
			},
		},
	}

	s1 := NewMemoryStore()
	assert.NotNil(t, s1)

	err := s1.Update(func(tx1 Tx) error {
		// Prepoulate nodes
		for _, n := range nodeSet {
			assert.NoError(t, tx1.Nodes().Create(n))
		}

		// Prepopulate jobs
		for _, j := range jobSet {
			assert.NoError(t, tx1.Jobs().Create(j))
		}

		// Prepopulate tasks
		for _, task := range taskSet {
			assert.NoError(t, tx1.Tasks().Create(task))
		}
		return nil
	})
	assert.NoError(t, err)

	// Fork
	s2 := NewMemoryStore()
	assert.NotNil(t, s2)
	watcher, err := s1.Snapshot(s2)
	defer s1.WatchQueue().StopWatch(watcher)
	assert.NoError(t, err)

	err = s2.View(func(tx2 ReadTx) error {
		assert.Equal(t, nodeSet[0], tx2.Nodes().Get("id1"))
		assert.Equal(t, nodeSet[1], tx2.Nodes().Get("id2"))
		assert.Equal(t, nodeSet[2], tx2.Nodes().Get("id3"))

		assert.Equal(t, jobSet[0], tx2.Jobs().Get("id1"))
		assert.Equal(t, jobSet[1], tx2.Jobs().Get("id2"))
		assert.Equal(t, jobSet[2], tx2.Jobs().Get("id3"))

		assert.Equal(t, taskSet[0], tx2.Tasks().Get("id1"))
		assert.Equal(t, taskSet[1], tx2.Tasks().Get("id2"))
		assert.Equal(t, taskSet[2], tx2.Tasks().Get("id3"))
		return nil
	})
	assert.NoError(t, err)

	// Create node
	createNode := &api.Node{
		ID: "id4",
		Spec: &api.NodeSpec{
			Meta: &api.Meta{
				Name: "name4",
			},
		},
	}

	err = s1.Update(func(tx1 Tx) error {
		assert.NoError(t, tx1.Nodes().Create(createNode))
		return nil
	})
	assert.NoError(t, err)

	assert.NoError(t, Apply(s2, <-watcher))

	err = s2.View(func(tx2 ReadTx) error {
		assert.Equal(t, createNode, tx2.Nodes().Get("id4"))
		return nil
	})
	assert.NoError(t, err)

	// Update node
	updateNode := &api.Node{
		ID: "id3",
		Spec: &api.NodeSpec{
			Meta: &api.Meta{
				Name: "name3",
			},
		},
	}

	err = s1.Update(func(tx1 Tx) error {
		assert.NoError(t, tx1.Nodes().Update(updateNode))
		return nil
	})
	assert.NoError(t, err)

	assert.NoError(t, Apply(s2, <-watcher))

	err = s2.View(func(tx2 ReadTx) error {
		assert.Equal(t, updateNode, tx2.Nodes().Get("id3"))
		return nil
	})
	assert.NoError(t, err)

	err = s1.Update(func(tx1 Tx) error {
		// Delete node
		assert.NoError(t, tx1.Nodes().Delete("id1"))
		return nil
	})
	assert.NoError(t, err)

	assert.NoError(t, Apply(s2, <-watcher))

	err = s2.View(func(tx2 ReadTx) error {
		assert.Nil(t, tx2.Nodes().Get("id1"))
		return nil
	})
	assert.NoError(t, err)

	// Create job
	createJob := &api.Job{
		ID: "id4",
		Spec: &api.JobSpec{
			Meta: &api.Meta{
				Name: "name4",
			},
		},
	}

	err = s1.Update(func(tx1 Tx) error {
		assert.NoError(t, tx1.Jobs().Create(createJob))
		return nil
	})
	assert.NoError(t, err)

	assert.NoError(t, Apply(s2, <-watcher))

	err = s2.View(func(tx2 ReadTx) error {
		assert.Equal(t, createJob, tx2.Jobs().Get("id4"))
		return nil
	})
	assert.NoError(t, err)

	// Update job
	updateJob := &api.Job{
		ID: "id3",
		Spec: &api.JobSpec{
			Meta: &api.Meta{
				Name: "name3",
			},
		},
	}

	err = s1.Update(func(tx1 Tx) error {
		assert.NotEqual(t, updateJob, tx1.Jobs().Get("id3"))
		assert.NoError(t, tx1.Jobs().Update(updateJob))
		return nil
	})
	assert.NoError(t, err)

	assert.NoError(t, Apply(s2, <-watcher))

	err = s2.View(func(tx2 ReadTx) error {
		assert.Equal(t, updateJob, tx2.Jobs().Get("id3"))
		return nil
	})
	assert.NoError(t, err)

	err = s1.Update(func(tx1 Tx) error {
		// Delete job
		assert.NoError(t, tx1.Jobs().Delete("id1"))
		return nil
	})
	assert.NoError(t, err)

	assert.NoError(t, Apply(s2, <-watcher))

	err = s2.View(func(tx2 ReadTx) error {
		assert.Nil(t, tx2.Jobs().Get("id1"))
		return nil
	})
	assert.NoError(t, err)

	// Create task
	createTask := &api.Task{
		ID: "id4",
		Spec: &api.JobSpec{
			Meta: &api.Meta{
				Name: "name4",
			},
		},
	}

	err = s1.Update(func(tx1 Tx) error {
		assert.NoError(t, tx1.Tasks().Create(createTask))
		return nil
	})
	assert.NoError(t, err)

	assert.NoError(t, Apply(s2, <-watcher))

	err = s2.View(func(tx2 ReadTx) error {
		assert.Equal(t, createTask, tx2.Tasks().Get("id4"))
		return nil
	})
	assert.NoError(t, err)

	// Update task
	updateTask := &api.Task{
		ID: "id3",
		Spec: &api.JobSpec{
			Meta: &api.Meta{
				Name: "name3",
			},
		},
	}

	err = s1.Update(func(tx1 Tx) error {
		assert.NoError(t, tx1.Tasks().Update(updateTask))
		return nil
	})
	assert.NoError(t, err)
	assert.NoError(t, Apply(s2, <-watcher))

	err = s2.View(func(tx2 ReadTx) error {
		assert.Equal(t, updateTask, tx2.Tasks().Get("id3"))
		return nil
	})
	assert.NoError(t, err)

	err = s1.Update(func(tx1 Tx) error {
		// Delete task
		assert.NoError(t, tx1.Tasks().Delete("id1"))
		return nil
	})
	assert.NoError(t, err)
	assert.NoError(t, Apply(s2, <-watcher))

	err = s2.View(func(tx2 ReadTx) error {
		assert.Nil(t, tx2.Tasks().Get("id1"))
		return nil
	})
	assert.NoError(t, err)
}

func TestFailedTransaction(t *testing.T) {
	s := NewMemoryStore()
	assert.NotNil(t, s)

	// Create one node
	err := s.Update(func(tx Tx) error {
		n := &api.Node{
			ID: "id1",
			Spec: &api.NodeSpec{
				Meta: &api.Meta{
					Name: "name1",
				},
			},
		}

		assert.NoError(t, tx.Nodes().Create(n))
		return nil
	})
	assert.NoError(t, err)

	// Create a second node, but then roll back the transaction
	err = s.Update(func(tx Tx) error {
		n := &api.Node{
			ID: "id2",
			Spec: &api.NodeSpec{
				Meta: &api.Meta{
					Name: "name2",
				},
			},
		}

		assert.NoError(t, tx.Nodes().Create(n))
		return errors.New("rollback")
	})
	assert.Error(t, err)

	err = s.View(func(tx ReadTx) error {
		foundNodes, err := tx.Nodes().Find(All)
		assert.NoError(t, err)
		assert.Len(t, foundNodes, 1)
		foundNodes, err = tx.Nodes().Find(ByName("name1"))
		assert.NoError(t, err)
		assert.Len(t, foundNodes, 1)
		foundNodes, err = tx.Nodes().Find(ByName("name2"))
		assert.NoError(t, err)
		assert.Len(t, foundNodes, 0)
		return nil
	})
	assert.NoError(t, err)
}

const benchmarkNumNodes = 10000

func setupNodes(b *testing.B, n int) (Store, []string) {
	s := NewMemoryStore()

	nodeIDs := make([]string, n)

	for i := 0; i < n; i++ {
		nodeIDs[i] = identity.NewID()
	}

	b.ResetTimer()

	_ = s.Update(func(tx1 Tx) error {
		for i := 0; i < n; i++ {
			_ = tx1.Nodes().Create(&api.Node{
				ID: nodeIDs[i],
				Spec: &api.NodeSpec{
					Meta: &api.Meta{
						Name: "name" + strconv.Itoa(i),
					},
				},
			})
		}
		return nil
	})

	return s, nodeIDs
}

func BenchmarkCreateNode(b *testing.B) {
	setupNodes(b, b.N)
}

func BenchmarkUpdateNode(b *testing.B) {
	s, nodeIDs := setupNodes(b, benchmarkNumNodes)
	b.ResetTimer()
	_ = s.Update(func(tx1 Tx) error {
		for i := 0; i < b.N; i++ {
			_ = tx1.Nodes().Update(&api.Node{
				ID: nodeIDs[i%benchmarkNumNodes],
				Spec: &api.NodeSpec{
					Meta: &api.Meta{
						Name: nodeIDs[i%benchmarkNumNodes] + "_" + strconv.Itoa(i),
					},
				},
			})
		}
		return nil
	})
}

func BenchmarkUpdateNodeTransaction(b *testing.B) {
	s, nodeIDs := setupNodes(b, benchmarkNumNodes)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = s.Update(func(tx1 Tx) error {
			_ = tx1.Nodes().Update(&api.Node{
				ID: nodeIDs[i%benchmarkNumNodes],
				Spec: &api.NodeSpec{
					Meta: &api.Meta{
						Name: nodeIDs[i%benchmarkNumNodes] + "_" + strconv.Itoa(i),
					},
				},
			})
			return nil
		})
	}
}

func BenchmarkDeleteNodeTransaction(b *testing.B) {
	s, nodeIDs := setupNodes(b, benchmarkNumNodes)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = s.Update(func(tx1 Tx) error {
			_ = tx1.Nodes().Delete(nodeIDs[0])
			// Don't actually commit deletions, so we can delete
			// things repeatedly to satisfy the benchmark structure.
			return errors.New("don't commit this")
		})
	}
}

func BenchmarkGetNode(b *testing.B) {
	s, nodeIDs := setupNodes(b, benchmarkNumNodes)
	b.ResetTimer()
	_ = s.View(func(tx1 ReadTx) error {
		for i := 0; i < b.N; i++ {
			_ = tx1.Nodes().Get(nodeIDs[i%benchmarkNumNodes])
		}
		return nil
	})
}

func BenchmarkFindAllNodes(b *testing.B) {
	s, _ := setupNodes(b, benchmarkNumNodes)
	b.ResetTimer()
	_ = s.View(func(tx1 ReadTx) error {
		for i := 0; i < b.N; i++ {
			_, _ = tx1.Nodes().Find(All)
		}
		return nil
	})
}

func BenchmarkFindNodeByName(b *testing.B) {
	s, _ := setupNodes(b, benchmarkNumNodes)
	b.ResetTimer()
	_ = s.View(func(tx1 ReadTx) error {
		for i := 0; i < b.N; i++ {
			_, _ = tx1.Nodes().Find(ByName("name" + strconv.Itoa(i)))
		}
		return nil
	})
}

func BenchmarkNodeConcurrency(b *testing.B) {
	s, nodeIDs := setupNodes(b, benchmarkNumNodes)
	b.ResetTimer()

	// Run 5 writer goroutines and 5 reader goroutines
	var wg sync.WaitGroup
	for c := 0; c != 5; c++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < b.N; i++ {
				_ = s.Update(func(tx1 Tx) error {
					_ = tx1.Nodes().Update(&api.Node{
						ID: nodeIDs[i%benchmarkNumNodes],
						Spec: &api.NodeSpec{
							Meta: &api.Meta{
								Name: nodeIDs[i%benchmarkNumNodes] + "_" + strconv.Itoa(c) + "_" + strconv.Itoa(i),
							},
						},
					})
					return nil
				})
			}
		}()
	}

	for c := 0; c != 5; c++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = s.View(func(tx1 ReadTx) error {
				for i := 0; i < b.N; i++ {
					_ = tx1.Nodes().Get(nodeIDs[i%benchmarkNumNodes])
				}
				return nil
			})
		}()
	}

	wg.Wait()
}
