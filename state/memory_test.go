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

	assert.Empty(t, s.Nodes())
	for _, n := range nodeSet {
		assert.NoError(t, s.CreateNode(n.Spec.ID, n))
	}
	assert.Len(t, s.Nodes(), len(nodeSet))

	assert.Error(t, s.CreateNode(nodeSet[0].Spec.ID, nodeSet[0]), "duplicate IDs must be rejected")

	assert.Equal(t, nodeSet[0], s.Node("id1"))
	assert.Equal(t, nodeSet[1], s.Node("id2"))
	assert.Equal(t, nodeSet[2], s.Node("id3"))

	assert.Len(t, s.NodesByName("name1"), 1)
	assert.Len(t, s.NodesByName("name2"), 2)
	assert.Len(t, s.NodesByName("invalid"), 0)

	// Update.
	update := &api.Node{
		Spec: &api.NodeSpec{
			ID: "id3",
			Meta: &api.Meta{
				Name: "name3",
			},
		},
	}
	assert.NotEqual(t, update, s.Node("id3"))
	assert.NoError(t, s.UpdateNode("id3", update))
	assert.Equal(t, update, s.Node("id3"))

	assert.Len(t, s.NodesByName("name2"), 1)
	assert.Len(t, s.NodesByName("name3"), 1)

	assert.Error(t, s.UpdateNode("invalid", nodeSet[0]), "invalid IDs should be rejected")

	// Delete
	assert.NotNil(t, s.Node("id1"))
	assert.NoError(t, s.DeleteNode("id1"))
	assert.Nil(t, s.Node("id1"))
	assert.Empty(t, s.NodesByName("name1"))
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

	assert.Empty(t, s.Jobs())
	for _, j := range jobSet {
		assert.NoError(t, s.CreateJob(j.ID, j))
	}
	assert.Len(t, s.Jobs(), len(jobSet))

	assert.Error(t, s.CreateJob(jobSet[0].ID, jobSet[0]), "duplicate IDs must be rejected")

	assert.Equal(t, jobSet[0], s.Job("id1"))
	assert.Equal(t, jobSet[1], s.Job("id2"))
	assert.Equal(t, jobSet[2], s.Job("id3"))

	assert.Len(t, s.JobsByName("name1"), 1)
	assert.Len(t, s.JobsByName("name2"), 2)
	assert.Len(t, s.JobsByName("invalid"), 0)

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
	assert.NotEqual(t, update, s.Job("id3"))
	assert.NoError(t, s.UpdateJob("id3", update))
	assert.Equal(t, update, s.Job("id3"))

	assert.Len(t, s.JobsByName("name2"), 1)
	assert.Len(t, s.JobsByName("name3"), 1)

	assert.Error(t, s.UpdateJob("invalid", jobSet[0]), "invalid IDs should be rejected")

	// Delete
	assert.NotNil(t, s.Job("id1"))
	assert.NoError(t, s.DeleteJob("id1"))
	assert.Nil(t, s.Job("id1"))
	assert.Empty(t, s.JobsByName("name1"))
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

	assert.NoError(t, s.CreateNode(node.Spec.ID, node))
	assert.NoError(t, s.CreateJob(job.ID, job))
	assert.Empty(t, s.Tasks())
	for _, task := range taskSet {
		assert.NoError(t, s.CreateTask(task.ID, task))
	}
	assert.Len(t, s.Tasks(), len(taskSet))

	assert.Error(t, s.CreateTask(taskSet[0].ID, taskSet[0]), "duplicate IDs must be rejected")

	assert.Equal(t, taskSet[0], s.Task("id1"))
	assert.Equal(t, taskSet[1], s.Task("id2"))
	assert.Equal(t, taskSet[2], s.Task("id3"))

	assert.Len(t, s.TasksByName("name1"), 1)
	assert.Len(t, s.TasksByName("name2"), 2)
	assert.Len(t, s.TasksByName("invalid"), 0)

	assert.Len(t, s.TasksByNode(node.Spec.ID), 1)
	assert.Equal(t, s.TasksByNode(node.Spec.ID)[0], taskSet[0])
	assert.Len(t, s.TasksByNode("invalid"), 0)

	assert.Len(t, s.TasksByJob(job.ID), 1)
	assert.Equal(t, s.TasksByJob(job.ID)[0], taskSet[1])
	assert.Len(t, s.TasksByJob("invalid"), 0)

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
	assert.NotEqual(t, update, s.Task("id3"))
	assert.NoError(t, s.UpdateTask("id3", update))
	assert.Equal(t, update, s.Task("id3"))

	assert.Len(t, s.TasksByName("name2"), 1)
	assert.Len(t, s.TasksByName("name3"), 1)

	assert.Error(t, s.UpdateTask("invalid", taskSet[0]), "invalid IDs should be rejected")

	// Delete
	assert.NotNil(t, s.Task("id1"))
	assert.NoError(t, s.DeleteTask("id1"))
	assert.Nil(t, s.Task("id1"))
	assert.Empty(t, s.TasksByName("name1"))
}

func TestStoreFork(t *testing.T) {
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

	// Prepoulate nodes
	for _, n := range nodeSet {
		assert.NoError(t, s1.CreateNode(n.Spec.ID, n))
	}

	// Prepopulate jobs
	for _, j := range jobSet {
		assert.NoError(t, s1.CreateJob(j.ID, j))
	}

	// Prepopulate tasks
	for _, task := range taskSet {
		assert.NoError(t, s1.CreateTask(task.ID, task))
	}

	// Fork
	s2 := NewMemoryStore()
	assert.NotNil(t, s2)
	watcher, err := s1.Fork(s2)
	defer s1.WatchQueue().StopWatch(watcher)
	assert.NoError(t, err)

	assert.Len(t, s2.Nodes(), len(nodeSet))

	assert.Equal(t, nodeSet[0], s2.Node("id1"))
	assert.Equal(t, nodeSet[1], s2.Node("id2"))
	assert.Equal(t, nodeSet[2], s2.Node("id3"))

	assert.Len(t, s2.Jobs(), len(jobSet))

	assert.Equal(t, jobSet[0], s2.Job("id1"))
	assert.Equal(t, jobSet[1], s2.Job("id2"))
	assert.Equal(t, jobSet[2], s2.Job("id3"))

	assert.Len(t, s2.JobsByName("name1"), 1)
	assert.Len(t, s2.JobsByName("name2"), 2)
	assert.Len(t, s2.JobsByName("invalid"), 0)

	assert.Len(t, s2.Tasks(), len(taskSet))

	assert.Equal(t, taskSet[0], s2.Task("id1"))
	assert.Equal(t, taskSet[1], s2.Task("id2"))
	assert.Equal(t, taskSet[2], s2.Task("id3"))

	assert.Len(t, s2.TasksByName("name1"), 1)
	assert.Len(t, s2.TasksByName("name2"), 2)
	assert.Len(t, s2.TasksByName("invalid"), 0)

	assert.Len(t, s2.TasksByNode(nodeSet[0].Spec.ID), 1)
	assert.Equal(t, s2.TasksByNode(nodeSet[0].Spec.ID)[0], taskSet[0])
	assert.Len(t, s2.TasksByNode("invalid"), 0)

	assert.Len(t, s2.TasksByJob(jobSet[0].ID), 1)
	assert.Equal(t, s2.TasksByJob(jobSet[0].ID)[0], taskSet[1])
	assert.Len(t, s2.TasksByJob("invalid"), 0)

	// Create node
	createNode := &api.Node{
		Spec: &api.NodeSpec{
			ID: "id4",
			Meta: &api.Meta{
				Name: "name4",
			},
		},
	}
	assert.NoError(t, s1.CreateNode("id4", createNode))
	assert.NoError(t, Apply(s2, <-watcher))
	assert.Equal(t, createNode, s2.Node("id4"))
	assert.Len(t, s2.NodesByName("name4"), 1)

	// Update node
	updateNode := &api.Node{
		Spec: &api.NodeSpec{
			ID: "id3",
			Meta: &api.Meta{
				Name: "name3",
			},
		},
	}
	assert.NoError(t, s1.UpdateNode("id3", updateNode))
	assert.NoError(t, Apply(s2, <-watcher))
	assert.Equal(t, updateNode, s2.Node("id3"))
	assert.Len(t, s2.NodesByName("name2"), 1)
	assert.Len(t, s2.NodesByName("name3"), 1)

	// Delete node
	assert.NotNil(t, s2.Node("id1"))
	assert.NoError(t, s1.DeleteNode("id1"))

	assert.NoError(t, Apply(s2, <-watcher))
	assert.Nil(t, s2.Node("id1"))
	assert.Empty(t, s2.NodesByName("name1"))

	// Create job
	createJob := &api.Job{
		ID: "id4",
		Spec: &api.JobSpec{
			Meta: &api.Meta{
				Name: "name4",
			},
		},
	}
	assert.NoError(t, s1.CreateJob("id4", createJob))
	assert.NoError(t, Apply(s2, <-watcher))
	assert.Equal(t, createJob, s2.Job("id4"))

	// Update job
	updateJob := &api.Job{
		ID: "id3",
		Spec: &api.JobSpec{
			Meta: &api.Meta{
				Name: "name3",
			},
		},
	}
	assert.NotEqual(t, updateJob, s1.Job("id3"))
	assert.NoError(t, s1.UpdateJob("id3", updateJob))
	assert.NoError(t, Apply(s2, <-watcher))
	assert.Equal(t, updateJob, s2.Job("id3"))

	assert.Len(t, s2.JobsByName("name2"), 1)
	assert.Len(t, s2.JobsByName("name3"), 1)

	// Delete job
	assert.NotNil(t, s1.Job("id1"))
	assert.NoError(t, s1.DeleteJob("id1"))
	assert.NoError(t, Apply(s2, <-watcher))
	assert.Nil(t, s2.Job("id1"))
	assert.Empty(t, s2.JobsByName("name1"))

	// Create task
	createTask := &api.Task{
		ID: "id4",
		Spec: &api.JobSpec{
			Meta: &api.Meta{
				Name: "name4",
			},
		},
	}
	assert.NoError(t, s1.CreateTask("id4", createTask))
	assert.NoError(t, Apply(s2, <-watcher))
	assert.Equal(t, createTask, s2.Task("id4"))

	// Update task
	updateTask := &api.Task{
		ID: "id3",
		Spec: &api.JobSpec{
			Meta: &api.Meta{
				Name: "name3",
			},
		},
	}
	assert.NotEqual(t, updateTask, s1.Task("id3"))
	assert.NoError(t, s1.UpdateTask("id3", updateTask))
	assert.NoError(t, Apply(s2, <-watcher))
	assert.Equal(t, updateTask, s2.Task("id3"))

	assert.Len(t, s2.TasksByName("name2"), 1)
	assert.Len(t, s2.TasksByName("name3"), 1)

	// Delete task
	assert.NotNil(t, s1.Task("id1"))
	assert.NoError(t, s1.DeleteTask("id1"))
	assert.NoError(t, Apply(s2, <-watcher))
	assert.Nil(t, s2.Task("id1"))
	assert.Empty(t, s2.TasksByName("name1"))
}
