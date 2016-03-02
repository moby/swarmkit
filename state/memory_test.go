package state

import (
	"testing"

	"github.com/docker/swarm-v2/api"
	"github.com/stretchr/testify/assert"
)

func TestStoreNode(t *testing.T) {
	nodeSet := []*api.Node{
		{
			Spec: &api.NodeSpec{
				ID: "id1",
				Meta: &api.Meta{
					Name: "name1",
				},
			},
		},
		{
			Spec: &api.NodeSpec{
				ID: "id2",
				Meta: &api.Meta{
					Name: "name2",
				},
			},
		},
		{
			Spec: &api.NodeSpec{
				ID: "id3",
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
		Spec: &api.NodeSpec{
			ID: "id3",
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
		invalidUpdate.Spec.ID = "invalid"
		assert.Error(t, tx.Nodes().Update(&invalidUpdate), "invalid IDs should be rejected")

		// Delete
		assert.NotNil(t, tx.Nodes().Get("id1"))
		assert.NoError(t, tx.Nodes().Delete("id1"))
		assert.Nil(t, tx.Nodes().Get("id1"))
		foundNodes, err = tx.Nodes().Find(ByName("name1"))
		assert.NoError(t, err)
		assert.Empty(t, foundNodes)
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
		return nil
	})
	assert.NoError(t, err)
}

func TestStoreTask(t *testing.T) {
	node := &api.Node{
		Spec: &api.NodeSpec{
			ID: "node1",
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
			NodeID: node.Spec.ID,
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

		foundTasks, err = readTx.Tasks().Find(ByNodeID(node.Spec.ID))
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
		return nil
	})
	assert.NoError(t, err)
}

func TestStoreSnapshot(t *testing.T) {
	nodeSet := []*api.Node{
		{
			Spec: &api.NodeSpec{
				ID: "id1",
				Meta: &api.Meta{
					Name: "name1",
				},
			},
		},
		{
			Spec: &api.NodeSpec{
				ID: "id2",
				Meta: &api.Meta{
					Name: "name2",
				},
			},
		},
		{
			Spec: &api.NodeSpec{
				ID: "id3",
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
			NodeID: nodeSet[0].Spec.ID,
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

	err = s1.Update(func(tx1 Tx) error {
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
			Spec: &api.NodeSpec{
				ID: "id4",
				Meta: &api.Meta{
					Name: "name4",
				},
			},
		}
		assert.NoError(t, tx1.Nodes().Create(createNode))
		assert.NoError(t, Apply(s2, <-watcher))

		err = s2.View(func(tx2 ReadTx) error {
			assert.Equal(t, createNode, tx2.Nodes().Get("id4"))
			return nil
		})
		assert.NoError(t, err)

		// Update node
		updateNode := &api.Node{
			Spec: &api.NodeSpec{
				ID: "id3",
				Meta: &api.Meta{
					Name: "name3",
				},
			},
		}
		assert.NoError(t, tx1.Nodes().Update(updateNode))
		assert.NoError(t, Apply(s2, <-watcher))

		err = s2.View(func(tx2 ReadTx) error {
			assert.Equal(t, updateNode, tx2.Nodes().Get("id3"))
			return nil
		})
		assert.NoError(t, err)

		// Delete node
		assert.NoError(t, tx1.Nodes().Delete("id1"))
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
		assert.NoError(t, tx1.Jobs().Create(createJob))
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
		assert.NotEqual(t, updateJob, tx1.Jobs().Get("id3"))
		assert.NoError(t, tx1.Jobs().Update(updateJob))
		assert.NoError(t, Apply(s2, <-watcher))

		err = s2.View(func(tx2 ReadTx) error {
			assert.Equal(t, updateJob, tx2.Jobs().Get("id3"))
			return nil
		})
		assert.NoError(t, err)

		// Delete job
		assert.NoError(t, tx1.Jobs().Delete("id1"))
		assert.NoError(t, Apply(s2, <-watcher))

		err = s2.View(func(tx2 ReadTx) error {
			assert.Nil(t, tx1.Jobs().Get("id1"))
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
		assert.NoError(t, tx1.Tasks().Create(createTask))
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
		assert.NoError(t, tx1.Tasks().Update(updateTask))
		assert.NoError(t, Apply(s2, <-watcher))

		err = s2.View(func(tx2 ReadTx) error {
			assert.Equal(t, updateTask, tx2.Tasks().Get("id3"))
			return nil
		})
		assert.NoError(t, err)

		// Delete task
		assert.NoError(t, tx1.Tasks().Delete("id1"))
		assert.NoError(t, Apply(s2, <-watcher))

		err = s2.View(func(tx2 ReadTx) error {
			assert.Nil(t, tx2.Tasks().Get("id1"))
			return nil
		})
		assert.NoError(t, err)
		return nil
	})
	assert.NoError(t, err)
}
