package state

import (
	"errors"
	"strconv"
	"sync"
	"testing"

	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/identity"
	"github.com/docker/swarm-v2/manager/state/pb"
	"github.com/stretchr/testify/assert"
	"golang.org/x/net/context"
)

var (
	nodeSet = []*api.Node{
		{
			ID: "id1",
			Spec: &api.NodeSpec{
				Meta: api.Meta{
					Name: "name1",
				},
			},
		},
		{
			ID: "id2",
			Spec: &api.NodeSpec{
				Meta: api.Meta{
					Name: "name2",
				},
			},
		},
		{
			ID: "id3",
			Spec: &api.NodeSpec{
				Meta: api.Meta{
					// intentionally conflicting name
					Name: "name2",
				},
			},
		},
	}

	jobSet = []*api.Job{
		{
			ID: "id1",
			Spec: &api.JobSpec{
				Meta: api.Meta{
					Name: "name1",
				},
			},
		},
		{
			ID: "id2",
			Spec: &api.JobSpec{
				Meta: api.Meta{
					Name: "name2",
				},
			},
		},
		{
			ID: "id3",
			Spec: &api.JobSpec{
				Meta: api.Meta{
					Name: "name3",
				},
			},
		},
	}

	taskSet = []*api.Task{
		{
			ID: "id1",
			Meta: api.Meta{
				Name: "name1",
			},
			Spec:   &api.TaskSpec{},
			NodeID: nodeSet[0].ID,
		},
		{
			ID: "id2",
			Meta: api.Meta{
				Name: "name2",
			},
			Spec:  &api.TaskSpec{},
			JobID: jobSet[0].ID,
		},
		{
			ID: "id3",
			Meta: api.Meta{
				Name: "name2",
			},
			Spec: &api.TaskSpec{},
		},
	}

	networkSet = []*api.Network{
		{
			ID: "id1",
			Spec: &api.NetworkSpec{
				Meta: api.Meta{
					Name: "name1",
				},
			},
		},
		{
			ID: "id2",
			Spec: &api.NetworkSpec{
				Meta: api.Meta{
					Name: "name2",
				},
			},
		},
		{
			ID: "id3",
			Spec: &api.NetworkSpec{
				Meta: api.Meta{
					// intentionally conflicting name
					Name: "name2",
				},
			},
		},
	}

	volumeSet = []*api.Volume{
		{
			ID: "id1",
			Spec: &api.VolumeSpec{
				Meta: api.Meta{
					Name: "name1",
				},
			},
		},
		{
			ID: "id2",
			Spec: &api.VolumeSpec{
				Meta: api.Meta{
					Name: "name2",
				},
			},
		},
		{
			ID: "id3",
			Spec: &api.VolumeSpec{
				Meta: api.Meta{
					Name: "name3",
				},
			},
		},
	}
)

func setupTestStore(t *testing.T, s Store) {
	err := s.Update(func(tx Tx) error {
		// Prepoulate nodes
		for _, n := range nodeSet {
			assert.NoError(t, tx.Nodes().Create(n))
		}

		// Prepopulate jobs
		for _, j := range jobSet {
			assert.NoError(t, tx.Jobs().Create(j))
		}
		// Prepopulate tasks
		for _, task := range taskSet {
			assert.NoError(t, tx.Tasks().Create(task))
		}
		// Prepopulate networks
		for _, n := range networkSet {
			assert.NoError(t, tx.Networks().Create(n))
		}
		// Prepopulate volumes
		for _, v := range volumeSet {
			assert.NoError(t, tx.Volumes().Create(v))
		}
		return nil
	})
	assert.NoError(t, err)
}

func TestStoreNode(t *testing.T) {
	s := NewMemoryStore(nil)
	assert.NotNil(t, s)

	err := s.View(func(readTx ReadTx) error {
		allNodes, err := readTx.Nodes().Find(All)
		assert.NoError(t, err)
		assert.Empty(t, allNodes)
		return nil
	})
	assert.NoError(t, err)

	setupTestStore(t, s)

	err = s.Update(func(tx Tx) error {
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

		foundNodes, err = readTx.Nodes().Find(ByQuery("name"))
		assert.NoError(t, err)
		assert.Len(t, foundNodes, 0)
		foundNodes, err = readTx.Nodes().Find(ByQuery("id"))
		assert.NoError(t, err)
		assert.Len(t, foundNodes, 3)

		return nil
	})
	assert.NoError(t, err)

	// Update.
	update := &api.Node{
		ID: "id3",
		Spec: &api.NodeSpec{
			Meta: api.Meta{
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
	s := NewMemoryStore(nil)
	assert.NotNil(t, s)

	err := s.View(func(readTx ReadTx) error {
		allJobs, err := readTx.Jobs().Find(All)
		assert.NoError(t, err)
		assert.Empty(t, allJobs)
		return nil
	})
	assert.NoError(t, err)

	setupTestStore(t, s)

	err = s.Update(func(tx Tx) error {
		assert.Equal(t,
			tx.Jobs().Create(&api.Job{
				ID: "id1",
				Spec: &api.JobSpec{
					Meta: api.Meta{
						Name: "name4",
					},
				},
			}), ErrExist, "duplicate IDs must be rejected")

		assert.Equal(t,
			tx.Jobs().Create(&api.Job{
				ID: "id4",
				Spec: &api.JobSpec{
					Meta: api.Meta{
						Name: "name1",
					},
				},
			}), ErrNameConflict, "duplicate names must be rejected")
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
		foundJobs, err = readTx.Jobs().Find(ByName("invalid"))
		assert.NoError(t, err)
		assert.Len(t, foundJobs, 0)

		foundJobs, err = readTx.Jobs().Find(ByQuery("name"))
		assert.NoError(t, err)
		assert.Len(t, foundJobs, 0)
		foundJobs, err = readTx.Jobs().Find(ByQuery("id"))
		assert.NoError(t, err)
		assert.Len(t, foundJobs, 3)

		return nil
	})
	assert.NoError(t, err)

	// Update.
	err = s.Update(func(tx Tx) error {
		// Regular update.
		update := jobSet[0].Copy()
		update.Spec.Meta.Labels = map[string]string{
			"foo": "bar",
		}

		assert.NotEqual(t, update, tx.Jobs().Get(update.ID))
		assert.NoError(t, tx.Jobs().Update(update))
		assert.Equal(t, update, tx.Jobs().Get(update.ID))

		// Name conflict.
		update = tx.Jobs().Get(update.ID)
		update.Spec.Meta.Name = "name2"
		assert.Equal(t, tx.Jobs().Update(update), ErrNameConflict, "duplicate names should be rejected")

		// Name change.
		update = tx.Jobs().Get(update.ID)
		foundJobs, err := tx.Jobs().Find(ByName("name1"))
		assert.NoError(t, err)
		assert.Len(t, foundJobs, 1)
		foundJobs, err = tx.Jobs().Find(ByName("name4"))
		assert.NoError(t, err)
		assert.Empty(t, foundJobs)

		update.Spec.Meta.Name = "name4"
		assert.NoError(t, tx.Jobs().Update(update))
		foundJobs, err = tx.Jobs().Find(ByName("name1"))
		assert.NoError(t, err)
		assert.Empty(t, foundJobs)
		foundJobs, err = tx.Jobs().Find(ByName("name4"))
		assert.NoError(t, err)
		assert.Len(t, foundJobs, 1)

		// Invalid update.
		invalidUpdate := jobSet[0].Copy()
		invalidUpdate.ID = "invalid"
		assert.Error(t, tx.Jobs().Update(invalidUpdate), "invalid IDs should be rejected")

		return nil
	})
	assert.NoError(t, err)

	// Delete
	err = s.Update(func(tx Tx) error {
		assert.NotNil(t, tx.Jobs().Get("id1"))
		assert.NoError(t, tx.Jobs().Delete("id1"))
		assert.Nil(t, tx.Jobs().Get("id1"))
		foundJobs, err := tx.Jobs().Find(ByName("name1"))
		assert.NoError(t, err)
		assert.Empty(t, foundJobs)

		assert.Equal(t, tx.Jobs().Delete("nonexistent"), ErrNotExist)
		return nil
	})
	assert.NoError(t, err)
}

func TestStoreNetwork(t *testing.T) {
	s := NewMemoryStore(nil)
	assert.NotNil(t, s)

	err := s.View(func(readTx ReadTx) error {
		allNetworks, err := readTx.Networks().Find(All)
		assert.NoError(t, err)
		assert.Empty(t, allNetworks)
		return nil
	})
	assert.NoError(t, err)

	setupTestStore(t, s)

	err = s.Update(func(tx Tx) error {
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
	s := NewMemoryStore(nil)
	assert.NotNil(t, s)

	err := s.View(func(tx ReadTx) error {
		allTasks, err := tx.Tasks().Find(All)
		assert.NoError(t, err)
		assert.Empty(t, allTasks)
		return nil
	})
	assert.NoError(t, err)

	setupTestStore(t, s)

	err = s.Update(func(tx Tx) error {
		allTasks, err := tx.Tasks().Find(All)
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

		foundTasks, err = readTx.Tasks().Find(ByNodeID(nodeSet[0].ID))
		assert.NoError(t, err)
		assert.Len(t, foundTasks, 1)
		assert.Equal(t, foundTasks[0], taskSet[0])
		foundTasks, err = readTx.Tasks().Find(ByNodeID("invalid"))
		assert.NoError(t, err)
		assert.Len(t, foundTasks, 0)

		foundTasks, err = readTx.Tasks().Find(ByJobID(jobSet[0].ID))
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
		Meta: api.Meta{
			Name: "name3",
		},
		Spec: &api.TaskSpec{},
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

func TestStoreVolume(t *testing.T) {
	s := NewMemoryStore(nil)
	assert.NotNil(t, s)

	err := s.View(func(readTx ReadTx) error {
		allVolumes, err := readTx.Volumes().Find(All)
		assert.NoError(t, err)
		assert.Empty(t, allVolumes)
		return nil
	})
	assert.NoError(t, err)

	setupTestStore(t, s)

	err = s.Update(func(tx Tx) error {
		assert.Equal(t,
			tx.Volumes().Create(&api.Volume{
				ID: "id1",
				Spec: &api.VolumeSpec{
					Meta: api.Meta{
						Name: "name4",
					},
				},
			}), ErrExist, "duplicate IDs must be rejected")

		assert.Equal(t,
			tx.Volumes().Create(&api.Volume{
				ID: "id4",
				Spec: &api.VolumeSpec{
					Meta: api.Meta{
						Name: "name1",
					},
				},
			}), ErrNameConflict, "duplicate names must be rejected")
		return nil
	})
	assert.NoError(t, err)

	err = s.View(func(readTx ReadTx) error {
		assert.Equal(t, volumeSet[0], readTx.Volumes().Get("id1"))
		assert.Equal(t, volumeSet[1], readTx.Volumes().Get("id2"))
		assert.Equal(t, volumeSet[2], readTx.Volumes().Get("id3"))

		foundVolumes, err := readTx.Volumes().Find(ByName("name1"))
		assert.NoError(t, err)
		assert.Len(t, foundVolumes, 1)
		foundVolumes, err = readTx.Volumes().Find(ByName("invalid"))
		assert.NoError(t, err)
		assert.Len(t, foundVolumes, 0)
		return nil
	})
	assert.NoError(t, err)

	// Update.
	err = s.Update(func(tx Tx) error {
		// Regular update.
		update := volumeSet[0].Copy()
		update.Spec.Meta.Labels = map[string]string{
			"foo": "bar",
		}

		assert.NotEqual(t, update, tx.Volumes().Get(update.ID))
		assert.NoError(t, tx.Volumes().Update(update))
		assert.Equal(t, update, tx.Volumes().Get(update.ID))

		// Name conflict.
		update = tx.Volumes().Get(update.ID)
		update.Spec.Meta.Name = "name2"
		assert.Equal(t, tx.Volumes().Update(update), ErrNameConflict, "duplicate names should be rejected")

		// Name change.
		update = tx.Volumes().Get(update.ID)
		foundVolumes, err := tx.Volumes().Find(ByName("name1"))
		assert.NoError(t, err)
		assert.Len(t, foundVolumes, 1)
		foundVolumes, err = tx.Volumes().Find(ByName("name4"))
		assert.NoError(t, err)
		assert.Empty(t, foundVolumes)

		update.Spec.Meta.Name = "name4"
		assert.NoError(t, tx.Volumes().Update(update))
		foundVolumes, err = tx.Volumes().Find(ByName("name1"))
		assert.NoError(t, err)
		assert.Empty(t, foundVolumes)
		foundVolumes, err = tx.Volumes().Find(ByName("name4"))
		assert.NoError(t, err)
		assert.Len(t, foundVolumes, 1)

		// Invalid update.
		invalidUpdate := volumeSet[0].Copy()
		invalidUpdate.ID = "invalid"
		assert.Error(t, tx.Volumes().Update(invalidUpdate), "invalid IDs should be rejected")

		return nil
	})
	assert.NoError(t, err)

	// Delete
	err = s.Update(func(tx Tx) error {
		assert.NotNil(t, tx.Volumes().Get("id1"))
		assert.NoError(t, tx.Volumes().Delete("id1"))
		assert.Nil(t, tx.Volumes().Get("id1"))
		foundVolumes, err := tx.Volumes().Find(ByName("name1"))
		assert.NoError(t, err)
		assert.Empty(t, foundVolumes)

		assert.Equal(t, tx.Volumes().Delete("nonexistent"), ErrNotExist)
		return nil
	})
	assert.NoError(t, err)
}

func TestStoreSnapshot(t *testing.T) {
	s1 := NewMemoryStore(nil)
	assert.NotNil(t, s1)

	setupTestStore(t, s1)

	// Fork
	s2 := NewMemoryStore(nil)
	assert.NotNil(t, s2)
	watcher, cancel, err := ViewAndWatch(s1, s2.CopyFrom)
	defer cancel()
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
			Meta: api.Meta{
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
	<-watcher // consume commit event

	err = s2.View(func(tx2 ReadTx) error {
		assert.Equal(t, createNode, tx2.Nodes().Get("id4"))
		return nil
	})
	assert.NoError(t, err)

	// Update node
	updateNode := &api.Node{
		ID: "id3",
		Spec: &api.NodeSpec{
			Meta: api.Meta{
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
	<-watcher // consume commit event

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
	<-watcher // consume commit event

	err = s2.View(func(tx2 ReadTx) error {
		assert.Nil(t, tx2.Nodes().Get("id1"))
		return nil
	})
	assert.NoError(t, err)

	// Create job
	createJob := &api.Job{
		ID: "id4",
		Spec: &api.JobSpec{
			Meta: api.Meta{
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
	<-watcher // consume commit event

	err = s2.View(func(tx2 ReadTx) error {
		assert.Equal(t, createJob, tx2.Jobs().Get("id4"))
		return nil
	})
	assert.NoError(t, err)

	// Update job
	updateJob := jobSet[2].Copy()
	updateJob.Spec.Meta.Name = "new-name"
	err = s1.Update(func(tx1 Tx) error {
		assert.NotEqual(t, updateJob, tx1.Jobs().Get(updateJob.ID))
		assert.NoError(t, tx1.Jobs().Update(updateJob))
		return nil
	})
	assert.NoError(t, err)

	assert.NoError(t, Apply(s2, <-watcher))
	<-watcher // consume commit event

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
	<-watcher // consume commit event

	err = s2.View(func(tx2 ReadTx) error {
		assert.Nil(t, tx2.Jobs().Get("id1"))
		return nil
	})
	assert.NoError(t, err)

	// Create task
	createTask := &api.Task{
		ID: "id4",
		Meta: api.Meta{
			Name: "name4",
		},
		Spec: &api.TaskSpec{},
	}

	err = s1.Update(func(tx1 Tx) error {
		assert.NoError(t, tx1.Tasks().Create(createTask))
		return nil
	})
	assert.NoError(t, err)

	assert.NoError(t, Apply(s2, <-watcher))
	<-watcher // consume commit event

	err = s2.View(func(tx2 ReadTx) error {
		assert.Equal(t, createTask, tx2.Tasks().Get("id4"))
		return nil
	})
	assert.NoError(t, err)

	// Update task
	updateTask := &api.Task{
		ID: "id3",
		Meta: api.Meta{
			Name: "name3",
		},
		Spec: &api.TaskSpec{},
	}

	err = s1.Update(func(tx1 Tx) error {
		assert.NoError(t, tx1.Tasks().Update(updateTask))
		return nil
	})
	assert.NoError(t, err)
	assert.NoError(t, Apply(s2, <-watcher))
	<-watcher // consume commit event

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
	<-watcher // consume commit event

	err = s2.View(func(tx2 ReadTx) error {
		assert.Nil(t, tx2.Tasks().Get("id1"))
		return nil
	})
	assert.NoError(t, err)
}

func TestFailedTransaction(t *testing.T) {
	s := NewMemoryStore(nil)
	assert.NotNil(t, s)

	// Create one node
	err := s.Update(func(tx Tx) error {
		n := &api.Node{
			ID: "id1",
			Spec: &api.NodeSpec{
				Meta: api.Meta{
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
				Meta: api.Meta{
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

type mockProposer struct {
	index uint64
}

func (mp *mockProposer) ProposeValue(ctx context.Context, storeAction []*api.StoreAction, cb func()) error {
	if cb != nil {
		cb()
	}
	return nil
}

func (mp *mockProposer) GetVersion() *api.Version {
	mp.index += 3
	return &api.Version{Index: mp.index}
}

func TestVersion(t *testing.T) {
	var mockProposer mockProposer
	s := NewMemoryStore(&mockProposer)
	assert.NotNil(t, s)

	var (
		retrievedNode  *api.Node
		retrievedNode2 *api.Node
	)

	// Create one node
	n := &api.Node{
		ID: "id1",
		Spec: &api.NodeSpec{
			Meta: api.Meta{
				Name: "name1",
			},
		},
	}
	err := s.Update(func(tx Tx) error {
		assert.NoError(t, tx.Nodes().Create(n))
		retrievedNode = tx.Nodes().Get(n.ID)
		return nil
	})
	assert.NoError(t, err)

	// Try to update the node without using an object fetched from the
	// store.
	n.Spec.Meta.Name = "name2"
	err = s.Update(func(tx Tx) error {
		assert.Equal(t, ErrSequenceConflict, tx.Nodes().Update(n))
		return nil
	})
	assert.NoError(t, err)

	// Try again, this time using the retrieved node.
	retrievedNode.Spec.Meta.Name = "name2"
	err = s.Update(func(tx Tx) error {
		assert.NoError(t, tx.Nodes().Update(retrievedNode))
		retrievedNode2 = tx.Nodes().Get(n.ID)
		return nil
	})
	assert.NoError(t, err)

	// Try to update retrievedNode again. This should fail because it was
	// already used to perform an update.
	retrievedNode.Spec.Meta.Name = "name3"
	err = s.Update(func(tx Tx) error {
		assert.Equal(t, ErrSequenceConflict, tx.Nodes().Update(n))
		return nil
	})
	assert.NoError(t, err)

	// But using retrievedNode2 should work, since it has the latest
	// sequence information.
	retrievedNode2.Spec.Meta.Name = "name3"
	err = s.Update(func(tx Tx) error {
		assert.NoError(t, tx.Nodes().Update(retrievedNode2))
		return nil
	})
	assert.NoError(t, err)

}

func TestStoreSaveRestore(t *testing.T) {
	s1 := NewMemoryStore(nil)
	assert.NotNil(t, s1)

	setupTestStore(t, s1)

	var snapshot *pb.StoreSnapshot
	err := s1.View(func(tx ReadTx) error {
		var err error
		snapshot, err = s1.Save(tx)
		assert.NoError(t, err)
		return nil
	})
	assert.NoError(t, err)

	s2 := NewMemoryStore(nil)
	assert.NotNil(t, s2)

	err = s2.Restore(snapshot)
	assert.NoError(t, err)

	err = s2.View(func(tx ReadTx) error {
		allTasks, err := tx.Tasks().Find(All)
		assert.NoError(t, err)
		assert.Len(t, allTasks, len(taskSet))
		for i := range allTasks {
			assert.Equal(t, allTasks[i], taskSet[i])
		}

		allNodes, err := tx.Nodes().Find(All)
		assert.NoError(t, err)
		assert.Len(t, allNodes, len(nodeSet))
		for i := range allNodes {
			assert.Equal(t, allNodes[i], nodeSet[i])
		}

		allNetworks, err := tx.Networks().Find(All)
		assert.NoError(t, err)
		assert.Len(t, allNetworks, len(networkSet))
		for i := range allNetworks {
			assert.Equal(t, allNetworks[i], networkSet[i])
		}

		allJobs, err := tx.Jobs().Find(All)
		assert.NoError(t, err)
		assert.Len(t, allJobs, len(jobSet))
		for i := range allJobs {
			assert.Equal(t, allJobs[i], jobSet[i])
		}

		return nil
	})
	assert.NoError(t, err)
}

const benchmarkNumNodes = 10000

func setupNodes(b *testing.B, n int) (Store, []string) {
	s := NewMemoryStore(nil)

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
					Meta: api.Meta{
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
					Meta: api.Meta{
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
					Meta: api.Meta{
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

func BenchmarkFindNodeByQuery(b *testing.B) {
	s, _ := setupNodes(b, benchmarkNumNodes)
	b.ResetTimer()
	_ = s.View(func(tx1 ReadTx) error {
		for i := 0; i < b.N; i++ {
			_, _ = tx1.Nodes().Find(ByQuery("name" + strconv.Itoa(i)))
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
							Meta: api.Meta{
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
