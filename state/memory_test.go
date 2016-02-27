package state

import (
	"testing"

	"github.com/docker/swarm-v2/api"
	"github.com/stretchr/testify/assert"
)

func TestStoreNode(t *testing.T) {
	nodeSet := []*api.Node{
		{
			ID: "id1",
			Meta: &api.Meta{
				Name: "name1",
			},
		},
		{
			ID: "id2",
			Meta: &api.Meta{
				Name: "name2",
			},
		},
		{
			ID: "id3",
			Meta: &api.Meta{
				Name: "name2",
			},
		},
	}

	s := NewMemoryStore()
	assert.NotNil(t, s)

	assert.Empty(t, s.Nodes())
	for _, n := range nodeSet {
		assert.NoError(t, s.CreateNode(n.ID, n))
	}
	assert.Len(t, s.Nodes(), len(nodeSet))

	assert.Error(t, s.CreateNode(nodeSet[0].ID, nodeSet[0]), "duplicate IDs must be rejected")

	assert.Equal(t, nodeSet[0], s.Node("id1"))
	assert.Equal(t, nodeSet[1], s.Node("id2"))
	assert.Equal(t, nodeSet[2], s.Node("id3"))

	assert.Len(t, s.NodesByName("name1"), 1)
	assert.Len(t, s.NodesByName("name2"), 2)
	assert.Len(t, s.NodesByName("invalid"), 0)

	// Update.
	update := &api.Node{
		ID: "id3",
		Meta: &api.Meta{
			Name: "name3",
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
			Meta: &api.Meta{
				Name: "name1",
			},
		},
		{
			ID: "id2",
			Meta: &api.Meta{
				Name: "name2",
			},
		},
		{
			ID: "id3",
			Meta: &api.Meta{
				Name: "name2",
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
		Meta: &api.Meta{
			Name: "name3",
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
		ID: "node1",
		Meta: &api.Meta{
			Name: "node-name1",
		},
	}
	job := &api.Job{
		ID: "job1",
		Meta: &api.Meta{
			Name: "job-name1",
		},
	}
	taskSet := []*api.Task{
		{
			ID: "id1",
			Meta: &api.Meta{
				Name: "name1",
			},
			NodeID: node.ID,
		},
		{
			ID: "id2",
			Meta: &api.Meta{
				Name: "name2",
			},
			JobID: job.ID,
		},
		{
			ID: "id3",
			Meta: &api.Meta{
				Name: "name2",
			},
		},
	}

	s := NewMemoryStore()
	assert.NotNil(t, s)

	assert.NoError(t, s.CreateNode(node.ID, node))
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

	assert.Len(t, s.TasksByNode(node.ID), 1)
	assert.Equal(t, s.TasksByNode(node.ID)[0], taskSet[0])
	assert.Len(t, s.TasksByNode("invalid"), 0)

	assert.Len(t, s.TasksByJob(job.ID), 1)
	assert.Equal(t, s.TasksByJob(job.ID)[0], taskSet[1])
	assert.Len(t, s.TasksByJob("invalid"), 0)

	// Update.
	update := &api.Task{
		ID: "id3",
		Meta: &api.Meta{
			Name: "name3",
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
