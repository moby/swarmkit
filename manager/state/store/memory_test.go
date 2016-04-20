package store

import (
	"errors"
	"strconv"
	"sync"
	"testing"

	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/identity"
	"github.com/docker/swarm-v2/manager/state"
	"github.com/docker/swarm-v2/manager/state/pb"
	"github.com/stretchr/testify/assert"
	"golang.org/x/net/context"
)

var (
	nodeSet = []*api.Node{
		{
			ID: "id1",
			Spec: &api.NodeSpec{
				Annotations: api.Annotations{
					Name: "name1",
				},
			},
		},
		{
			ID: "id2",
			Spec: &api.NodeSpec{
				Annotations: api.Annotations{
					Name: "name2",
				},
			},
		},
		{
			ID: "id3",
			Spec: &api.NodeSpec{
				Annotations: api.Annotations{
					// intentionally conflicting name
					Name: "name2",
				},
			},
		},
	}

	serviceSet = []*api.Service{
		{
			ID: "id1",
			Spec: &api.ServiceSpec{
				Annotations: api.Annotations{
					Name: "name1",
				},
			},
		},
		{
			ID: "id2",
			Spec: &api.ServiceSpec{
				Annotations: api.Annotations{
					Name: "name2",
				},
			},
		},
		{
			ID: "id3",
			Spec: &api.ServiceSpec{
				Annotations: api.Annotations{
					Name: "name3",
				},
			},
		},
	}

	taskSet = []*api.Task{
		{
			ID: "id1",
			Annotations: api.Annotations{
				Name: "name1",
			},
			Spec:   &api.TaskSpec{},
			NodeID: nodeSet[0].ID,
		},
		{
			ID: "id2",
			Annotations: api.Annotations{
				Name: "name2",
			},
			Spec:      &api.TaskSpec{},
			ServiceID: serviceSet[0].ID,
		},
		{
			ID: "id3",
			Annotations: api.Annotations{
				Name: "name2",
			},
			Spec: &api.TaskSpec{},
		},
	}

	networkSet = []*api.Network{
		{
			ID: "id1",
			Spec: &api.NetworkSpec{
				Annotations: api.Annotations{
					Name: "name1",
				},
			},
		},
		{
			ID: "id2",
			Spec: &api.NetworkSpec{
				Annotations: api.Annotations{
					Name: "name2",
				},
			},
		},
		{
			ID: "id3",
			Spec: &api.NetworkSpec{
				Annotations: api.Annotations{
					Name: "name3",
				},
			},
		},
	}

	volumeSet = []*api.Volume{
		{
			ID: "id1",
			Spec: &api.VolumeSpec{
				Annotations: api.Annotations{
					Name: "name1",
				},
			},
		},
		{
			ID: "id2",
			Spec: &api.VolumeSpec{
				Annotations: api.Annotations{
					Name: "name2",
				},
			},
		},
		{
			ID: "id3",
			Spec: &api.VolumeSpec{
				Annotations: api.Annotations{
					Name: "name3",
				},
			},
		},
	}
)

func setupTestStore(t *testing.T, s state.Store) {
	err := s.Update(func(tx state.Tx) error {
		// Prepoulate nodes
		for _, n := range nodeSet {
			assert.NoError(t, tx.Nodes().Create(n))
		}

		// Prepopulate services
		for _, j := range serviceSet {
			assert.NoError(t, tx.Services().Create(j))
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

	err := s.View(func(readTx state.ReadTx) error {
		allNodes, err := readTx.Nodes().Find(state.All)
		assert.NoError(t, err)
		assert.Empty(t, allNodes)
		return nil
	})
	assert.NoError(t, err)

	setupTestStore(t, s)

	err = s.Update(func(tx state.Tx) error {
		allNodes, err := tx.Nodes().Find(state.All)
		assert.NoError(t, err)
		assert.Len(t, allNodes, len(nodeSet))

		assert.Error(t, tx.Nodes().Create(nodeSet[0]), "duplicate IDs must be rejected")
		return nil
	})
	assert.NoError(t, err)

	err = s.View(func(readTx state.ReadTx) error {
		assert.Equal(t, nodeSet[0], readTx.Nodes().Get("id1"))
		assert.Equal(t, nodeSet[1], readTx.Nodes().Get("id2"))
		assert.Equal(t, nodeSet[2], readTx.Nodes().Get("id3"))

		foundNodes, err := readTx.Nodes().Find(state.ByName("name1"))
		assert.NoError(t, err)
		assert.Len(t, foundNodes, 1)
		foundNodes, err = readTx.Nodes().Find(state.ByName("name2"))
		assert.NoError(t, err)
		assert.Len(t, foundNodes, 2)
		foundNodes, err = readTx.Nodes().Find(state.ByName("invalid"))
		assert.NoError(t, err)
		assert.Len(t, foundNodes, 0)

		foundNodes, err = readTx.Nodes().Find(state.ByQuery("name"))
		assert.NoError(t, err)
		assert.Len(t, foundNodes, 0)
		foundNodes, err = readTx.Nodes().Find(state.ByQuery("id"))
		assert.NoError(t, err)
		assert.Len(t, foundNodes, 3)

		return nil
	})
	assert.NoError(t, err)

	// Update.
	update := &api.Node{
		ID: "id3",
		Spec: &api.NodeSpec{
			Annotations: api.Annotations{
				Name: "name3",
			},
		},
	}
	err = s.Update(func(tx state.Tx) error {
		assert.NotEqual(t, update, tx.Nodes().Get("id3"))
		assert.NoError(t, tx.Nodes().Update(update))
		assert.Equal(t, update, tx.Nodes().Get("id3"))

		foundNodes, err := tx.Nodes().Find(state.ByName("name2"))
		assert.NoError(t, err)
		assert.Len(t, foundNodes, 1)
		foundNodes, err = tx.Nodes().Find(state.ByName("name3"))
		assert.NoError(t, err)
		assert.Len(t, foundNodes, 1)

		invalidUpdate := *nodeSet[0]
		invalidUpdate.ID = "invalid"
		assert.Error(t, tx.Nodes().Update(&invalidUpdate), "invalid IDs should be rejected")

		// Delete
		assert.NotNil(t, tx.Nodes().Get("id1"))
		assert.NoError(t, tx.Nodes().Delete("id1"))
		assert.Nil(t, tx.Nodes().Get("id1"))
		foundNodes, err = tx.Nodes().Find(state.ByName("name1"))
		assert.NoError(t, err)
		assert.Empty(t, foundNodes)

		assert.Equal(t, tx.Nodes().Delete("nonexistent"), state.ErrNotExist)
		return nil
	})
	assert.NoError(t, err)
}

func TestStoreService(t *testing.T) {
	s := NewMemoryStore(nil)
	assert.NotNil(t, s)

	err := s.View(func(readTx state.ReadTx) error {
		allServices, err := readTx.Services().Find(state.All)
		assert.NoError(t, err)
		assert.Empty(t, allServices)
		return nil
	})
	assert.NoError(t, err)

	setupTestStore(t, s)

	err = s.Update(func(tx state.Tx) error {
		assert.Equal(t,
			tx.Services().Create(&api.Service{
				ID: "id1",
				Spec: &api.ServiceSpec{
					Annotations: api.Annotations{
						Name: "name4",
					},
				},
			}), state.ErrExist, "duplicate IDs must be rejected")

		assert.Equal(t,
			tx.Services().Create(&api.Service{
				ID: "id4",
				Spec: &api.ServiceSpec{
					Annotations: api.Annotations{
						Name: "name1",
					},
				},
			}), state.ErrNameConflict, "duplicate names must be rejected")
		return nil
	})
	assert.NoError(t, err)

	err = s.View(func(readTx state.ReadTx) error {
		assert.Equal(t, serviceSet[0], readTx.Services().Get("id1"))
		assert.Equal(t, serviceSet[1], readTx.Services().Get("id2"))
		assert.Equal(t, serviceSet[2], readTx.Services().Get("id3"))

		foundServices, err := readTx.Services().Find(state.ByName("name1"))
		assert.NoError(t, err)
		assert.Len(t, foundServices, 1)
		foundServices, err = readTx.Services().Find(state.ByName("invalid"))
		assert.NoError(t, err)
		assert.Len(t, foundServices, 0)

		foundServices, err = readTx.Services().Find(state.ByQuery("name"))
		assert.NoError(t, err)
		assert.Len(t, foundServices, 0)
		foundServices, err = readTx.Services().Find(state.ByQuery("id"))
		assert.NoError(t, err)
		assert.Len(t, foundServices, 3)

		return nil
	})
	assert.NoError(t, err)

	// Update.
	err = s.Update(func(tx state.Tx) error {
		// Regular update.
		update := serviceSet[0].Copy()
		update.Spec.Annotations.Labels = map[string]string{
			"foo": "bar",
		}

		assert.NotEqual(t, update, tx.Services().Get(update.ID))
		assert.NoError(t, tx.Services().Update(update))
		assert.Equal(t, update, tx.Services().Get(update.ID))

		// Name conflict.
		update = tx.Services().Get(update.ID)
		update.Spec.Annotations.Name = "name2"
		assert.Equal(t, tx.Services().Update(update), state.ErrNameConflict, "duplicate names should be rejected")

		// Name change.
		update = tx.Services().Get(update.ID)
		foundServices, err := tx.Services().Find(state.ByName("name1"))
		assert.NoError(t, err)
		assert.Len(t, foundServices, 1)
		foundServices, err = tx.Services().Find(state.ByName("name4"))
		assert.NoError(t, err)
		assert.Empty(t, foundServices)

		update.Spec.Annotations.Name = "name4"
		assert.NoError(t, tx.Services().Update(update))
		foundServices, err = tx.Services().Find(state.ByName("name1"))
		assert.NoError(t, err)
		assert.Empty(t, foundServices)
		foundServices, err = tx.Services().Find(state.ByName("name4"))
		assert.NoError(t, err)
		assert.Len(t, foundServices, 1)

		// Invalid update.
		invalidUpdate := serviceSet[0].Copy()
		invalidUpdate.ID = "invalid"
		assert.Error(t, tx.Services().Update(invalidUpdate), "invalid IDs should be rejected")

		return nil
	})
	assert.NoError(t, err)

	// Delete
	err = s.Update(func(tx state.Tx) error {
		assert.NotNil(t, tx.Services().Get("id1"))
		assert.NoError(t, tx.Services().Delete("id1"))
		assert.Nil(t, tx.Services().Get("id1"))
		foundServices, err := tx.Services().Find(state.ByName("name1"))
		assert.NoError(t, err)
		assert.Empty(t, foundServices)

		assert.Equal(t, tx.Services().Delete("nonexistent"), state.ErrNotExist)
		return nil
	})
	assert.NoError(t, err)
}

func TestStoreNetwork(t *testing.T) {
	s := NewMemoryStore(nil)
	assert.NotNil(t, s)

	err := s.View(func(readTx state.ReadTx) error {
		allNetworks, err := readTx.Networks().Find(state.All)
		assert.NoError(t, err)
		assert.Empty(t, allNetworks)
		return nil
	})
	assert.NoError(t, err)

	setupTestStore(t, s)

	err = s.Update(func(tx state.Tx) error {
		allNetworks, err := tx.Networks().Find(state.All)
		assert.NoError(t, err)
		assert.Len(t, allNetworks, len(networkSet))

		assert.Error(t, tx.Networks().Create(networkSet[0]), "duplicate IDs must be rejected")
		return nil
	})
	assert.NoError(t, err)

	err = s.View(func(readTx state.ReadTx) error {
		assert.Equal(t, networkSet[0], readTx.Networks().Get("id1"))
		assert.Equal(t, networkSet[1], readTx.Networks().Get("id2"))
		assert.Equal(t, networkSet[2], readTx.Networks().Get("id3"))

		foundNetworks, err := readTx.Networks().Find(state.ByName("name1"))
		assert.NoError(t, err)
		assert.Len(t, foundNetworks, 1)
		foundNetworks, err = readTx.Networks().Find(state.ByName("name2"))
		assert.NoError(t, err)
		assert.Len(t, foundNetworks, 1)
		foundNetworks, err = readTx.Networks().Find(state.ByName("invalid"))
		assert.NoError(t, err)
		assert.Len(t, foundNetworks, 0)
		return nil
	})
	assert.NoError(t, err)

	err = s.Update(func(tx state.Tx) error {
		// Delete
		assert.NotNil(t, tx.Networks().Get("id1"))
		assert.NoError(t, tx.Networks().Delete("id1"))
		assert.Nil(t, tx.Networks().Get("id1"))
		foundNetworks, err := tx.Networks().Find(state.ByName("name1"))
		assert.NoError(t, err)
		assert.Empty(t, foundNetworks)

		assert.Equal(t, tx.Tasks().Delete("nonexistent"), state.ErrNotExist)
		return nil
	})

	assert.NoError(t, err)
}

func TestStoreTask(t *testing.T) {
	s := NewMemoryStore(nil)
	assert.NotNil(t, s)

	err := s.View(func(tx state.ReadTx) error {
		allTasks, err := tx.Tasks().Find(state.All)
		assert.NoError(t, err)
		assert.Empty(t, allTasks)
		return nil
	})
	assert.NoError(t, err)

	setupTestStore(t, s)

	err = s.Update(func(tx state.Tx) error {
		allTasks, err := tx.Tasks().Find(state.All)
		assert.NoError(t, err)
		assert.Len(t, allTasks, len(taskSet))

		assert.Error(t, tx.Tasks().Create(taskSet[0]), "duplicate IDs must be rejected")
		return nil
	})
	assert.NoError(t, err)

	err = s.View(func(readTx state.ReadTx) error {
		assert.Equal(t, taskSet[0], readTx.Tasks().Get("id1"))
		assert.Equal(t, taskSet[1], readTx.Tasks().Get("id2"))
		assert.Equal(t, taskSet[2], readTx.Tasks().Get("id3"))

		foundTasks, err := readTx.Tasks().Find(state.ByName("name1"))
		assert.NoError(t, err)
		assert.Len(t, foundTasks, 1)
		foundTasks, err = readTx.Tasks().Find(state.ByName("name2"))
		assert.NoError(t, err)
		assert.Len(t, foundTasks, 2)
		foundTasks, err = readTx.Tasks().Find(state.ByName("invalid"))
		assert.NoError(t, err)
		assert.Len(t, foundTasks, 0)

		foundTasks, err = readTx.Tasks().Find(state.ByNodeID(nodeSet[0].ID))
		assert.NoError(t, err)
		assert.Len(t, foundTasks, 1)
		assert.Equal(t, foundTasks[0], taskSet[0])
		foundTasks, err = readTx.Tasks().Find(state.ByNodeID("invalid"))
		assert.NoError(t, err)
		assert.Len(t, foundTasks, 0)

		foundTasks, err = readTx.Tasks().Find(state.ByServiceID(serviceSet[0].ID))
		assert.NoError(t, err)
		assert.Len(t, foundTasks, 1)
		assert.Equal(t, foundTasks[0], taskSet[1])
		foundTasks, err = readTx.Tasks().Find(state.ByServiceID("invalid"))
		assert.NoError(t, err)
		assert.Len(t, foundTasks, 0)
		return nil
	})
	assert.NoError(t, err)

	// Update.
	update := &api.Task{
		ID: "id3",
		Annotations: api.Annotations{
			Name: "name3",
		},
		Spec: &api.TaskSpec{},
	}
	err = s.Update(func(tx state.Tx) error {
		assert.NotEqual(t, update, tx.Tasks().Get("id3"))
		assert.NoError(t, tx.Tasks().Update(update))
		assert.Equal(t, update, tx.Tasks().Get("id3"))

		foundTasks, err := tx.Tasks().Find(state.ByName("name2"))
		assert.NoError(t, err)
		assert.Len(t, foundTasks, 1)
		foundTasks, err = tx.Tasks().Find(state.ByName("name3"))
		assert.NoError(t, err)
		assert.Len(t, foundTasks, 1)

		invalidUpdate := *taskSet[0]
		invalidUpdate.ID = "invalid"
		assert.Error(t, tx.Tasks().Update(&invalidUpdate), "invalid IDs should be rejected")

		// Delete
		assert.NotNil(t, tx.Tasks().Get("id1"))
		assert.NoError(t, tx.Tasks().Delete("id1"))
		assert.Nil(t, tx.Tasks().Get("id1"))
		foundTasks, err = tx.Tasks().Find(state.ByName("name1"))
		assert.NoError(t, err)
		assert.Empty(t, foundTasks)

		assert.Equal(t, tx.Tasks().Delete("nonexistent"), state.ErrNotExist)
		return nil
	})
	assert.NoError(t, err)
}

func TestStoreVolume(t *testing.T) {
	s := NewMemoryStore(nil)
	assert.NotNil(t, s)

	err := s.View(func(readTx state.ReadTx) error {
		allVolumes, err := readTx.Volumes().Find(state.All)
		assert.NoError(t, err)
		assert.Empty(t, allVolumes)
		return nil
	})
	assert.NoError(t, err)

	setupTestStore(t, s)

	err = s.Update(func(tx state.Tx) error {
		assert.Equal(t,
			tx.Volumes().Create(&api.Volume{
				ID: "id1",
				Spec: &api.VolumeSpec{
					Annotations: api.Annotations{
						Name: "name4",
					},
				},
			}), state.ErrExist, "duplicate IDs must be rejected")

		assert.Equal(t,
			tx.Volumes().Create(&api.Volume{
				ID: "id4",
				Spec: &api.VolumeSpec{
					Annotations: api.Annotations{
						Name: "name1",
					},
				},
			}), state.ErrNameConflict, "duplicate names must be rejected")
		return nil
	})
	assert.NoError(t, err)

	err = s.View(func(readTx state.ReadTx) error {
		assert.Equal(t, volumeSet[0], readTx.Volumes().Get("id1"))
		assert.Equal(t, volumeSet[1], readTx.Volumes().Get("id2"))
		assert.Equal(t, volumeSet[2], readTx.Volumes().Get("id3"))

		foundVolumes, err := readTx.Volumes().Find(state.ByName("name1"))
		assert.NoError(t, err)
		assert.Len(t, foundVolumes, 1)
		foundVolumes, err = readTx.Volumes().Find(state.ByName("invalid"))
		assert.NoError(t, err)
		assert.Len(t, foundVolumes, 0)
		return nil
	})
	assert.NoError(t, err)

	// Update.
	err = s.Update(func(tx state.Tx) error {
		// Regular update.
		update := volumeSet[0].Copy()
		update.Spec.Annotations.Labels = map[string]string{
			"foo": "bar",
		}

		assert.NotEqual(t, update, tx.Volumes().Get(update.ID))
		assert.NoError(t, tx.Volumes().Update(update))
		assert.Equal(t, update, tx.Volumes().Get(update.ID))

		// Name conflict.
		update = tx.Volumes().Get(update.ID)
		update.Spec.Annotations.Name = "name2"
		assert.Equal(t, tx.Volumes().Update(update), state.ErrNameConflict, "duplicate names should be rejected")

		// Name change.
		update = tx.Volumes().Get(update.ID)
		foundVolumes, err := tx.Volumes().Find(state.ByName("name1"))
		assert.NoError(t, err)
		assert.Len(t, foundVolumes, 1)
		foundVolumes, err = tx.Volumes().Find(state.ByName("name4"))
		assert.NoError(t, err)
		assert.Empty(t, foundVolumes)

		update.Spec.Annotations.Name = "name4"
		assert.NoError(t, tx.Volumes().Update(update))
		foundVolumes, err = tx.Volumes().Find(state.ByName("name1"))
		assert.NoError(t, err)
		assert.Empty(t, foundVolumes)
		foundVolumes, err = tx.Volumes().Find(state.ByName("name4"))
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
	err = s.Update(func(tx state.Tx) error {
		assert.NotNil(t, tx.Volumes().Get("id1"))
		assert.NoError(t, tx.Volumes().Delete("id1"))
		assert.Nil(t, tx.Volumes().Get("id1"))
		foundVolumes, err := tx.Volumes().Find(state.ByName("name1"))
		assert.NoError(t, err)
		assert.Empty(t, foundVolumes)

		assert.Equal(t, tx.Volumes().Delete("nonexistent"), state.ErrNotExist)
		return nil
	})
	assert.NoError(t, err)
}

func TestStoreSnapshot(t *testing.T) {
	s1 := NewMemoryStore(nil)
	assert.NotNil(t, s1)

	setupTestStore(t, s1)

	s2 := NewMemoryStore(nil)
	assert.NotNil(t, s2)

	copyToS2 := func(readTx state.ReadTx) error {
		return s2.Update(func(tx state.Tx) error {
			// Copy over new data
			nodes, err := readTx.Nodes().Find(state.All)
			if err != nil {
				return err
			}
			for _, n := range nodes {
				if err := tx.Nodes().Create(n); err != nil {
					return err
				}
			}

			tasks, err := readTx.Tasks().Find(state.All)
			if err != nil {
				return err
			}
			for _, t := range tasks {
				if err := tx.Tasks().Create(t); err != nil {
					return err
				}
			}

			services, err := readTx.Services().Find(state.All)
			if err != nil {
				return err
			}
			for _, j := range services {
				if err := tx.Services().Create(j); err != nil {
					return err
				}
			}

			networks, err := readTx.Networks().Find(state.All)
			if err != nil {
				return err
			}
			for _, n := range networks {
				if err := tx.Networks().Create(n); err != nil {
					return err
				}
			}

			volumes, err := readTx.Volumes().Find(state.All)
			if err != nil {
				return err
			}
			for _, v := range volumes {
				if err := tx.Volumes().Create(v); err != nil {
					return err
				}
			}

			return nil
		})
	}

	// Fork
	watcher, cancel, err := state.ViewAndWatch(s1, copyToS2)
	defer cancel()
	assert.NoError(t, err)

	err = s2.View(func(tx2 state.ReadTx) error {
		assert.Equal(t, nodeSet[0], tx2.Nodes().Get("id1"))
		assert.Equal(t, nodeSet[1], tx2.Nodes().Get("id2"))
		assert.Equal(t, nodeSet[2], tx2.Nodes().Get("id3"))

		assert.Equal(t, serviceSet[0], tx2.Services().Get("id1"))
		assert.Equal(t, serviceSet[1], tx2.Services().Get("id2"))
		assert.Equal(t, serviceSet[2], tx2.Services().Get("id3"))

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
			Annotations: api.Annotations{
				Name: "name4",
			},
		},
	}

	err = s1.Update(func(tx1 state.Tx) error {
		assert.NoError(t, tx1.Nodes().Create(createNode))
		return nil
	})
	assert.NoError(t, err)

	assert.NoError(t, state.Apply(s2, <-watcher))
	<-watcher // consume commit event

	err = s2.View(func(tx2 state.ReadTx) error {
		assert.Equal(t, createNode, tx2.Nodes().Get("id4"))
		return nil
	})
	assert.NoError(t, err)

	// Update node
	updateNode := &api.Node{
		ID: "id3",
		Spec: &api.NodeSpec{
			Annotations: api.Annotations{
				Name: "name3",
			},
		},
	}

	err = s1.Update(func(tx1 state.Tx) error {
		assert.NoError(t, tx1.Nodes().Update(updateNode))
		return nil
	})
	assert.NoError(t, err)

	assert.NoError(t, state.Apply(s2, <-watcher))
	<-watcher // consume commit event

	err = s2.View(func(tx2 state.ReadTx) error {
		assert.Equal(t, updateNode, tx2.Nodes().Get("id3"))
		return nil
	})
	assert.NoError(t, err)

	err = s1.Update(func(tx1 state.Tx) error {
		// Delete node
		assert.NoError(t, tx1.Nodes().Delete("id1"))
		return nil
	})
	assert.NoError(t, err)

	assert.NoError(t, state.Apply(s2, <-watcher))
	<-watcher // consume commit event

	err = s2.View(func(tx2 state.ReadTx) error {
		assert.Nil(t, tx2.Nodes().Get("id1"))
		return nil
	})
	assert.NoError(t, err)

	// Create service
	createService := &api.Service{
		ID: "id4",
		Spec: &api.ServiceSpec{
			Annotations: api.Annotations{
				Name: "name4",
			},
		},
	}

	err = s1.Update(func(tx1 state.Tx) error {
		assert.NoError(t, tx1.Services().Create(createService))
		return nil
	})
	assert.NoError(t, err)

	assert.NoError(t, state.Apply(s2, <-watcher))
	<-watcher // consume commit event

	err = s2.View(func(tx2 state.ReadTx) error {
		assert.Equal(t, createService, tx2.Services().Get("id4"))
		return nil
	})
	assert.NoError(t, err)

	// Update service
	updateService := serviceSet[2].Copy()
	updateService.Spec.Annotations.Name = "new-name"
	err = s1.Update(func(tx1 state.Tx) error {
		assert.NotEqual(t, updateService, tx1.Services().Get(updateService.ID))
		assert.NoError(t, tx1.Services().Update(updateService))
		return nil
	})
	assert.NoError(t, err)

	assert.NoError(t, state.Apply(s2, <-watcher))
	<-watcher // consume commit event

	err = s2.View(func(tx2 state.ReadTx) error {
		assert.Equal(t, updateService, tx2.Services().Get("id3"))
		return nil
	})
	assert.NoError(t, err)

	err = s1.Update(func(tx1 state.Tx) error {
		// Delete service
		assert.NoError(t, tx1.Services().Delete("id1"))
		return nil
	})
	assert.NoError(t, err)

	assert.NoError(t, state.Apply(s2, <-watcher))
	<-watcher // consume commit event

	err = s2.View(func(tx2 state.ReadTx) error {
		assert.Nil(t, tx2.Services().Get("id1"))
		return nil
	})
	assert.NoError(t, err)

	// Create task
	createTask := &api.Task{
		ID: "id4",
		Annotations: api.Annotations{
			Name: "name4",
		},
		Spec: &api.TaskSpec{},
	}

	err = s1.Update(func(tx1 state.Tx) error {
		assert.NoError(t, tx1.Tasks().Create(createTask))
		return nil
	})
	assert.NoError(t, err)

	assert.NoError(t, state.Apply(s2, <-watcher))
	<-watcher // consume commit event

	err = s2.View(func(tx2 state.ReadTx) error {
		assert.Equal(t, createTask, tx2.Tasks().Get("id4"))
		return nil
	})
	assert.NoError(t, err)

	// Update task
	updateTask := &api.Task{
		ID: "id3",
		Annotations: api.Annotations{
			Name: "name3",
		},
		Spec: &api.TaskSpec{},
	}

	err = s1.Update(func(tx1 state.Tx) error {
		assert.NoError(t, tx1.Tasks().Update(updateTask))
		return nil
	})
	assert.NoError(t, err)
	assert.NoError(t, state.Apply(s2, <-watcher))
	<-watcher // consume commit event

	err = s2.View(func(tx2 state.ReadTx) error {
		assert.Equal(t, updateTask, tx2.Tasks().Get("id3"))
		return nil
	})
	assert.NoError(t, err)

	err = s1.Update(func(tx1 state.Tx) error {
		// Delete task
		assert.NoError(t, tx1.Tasks().Delete("id1"))
		return nil
	})
	assert.NoError(t, err)
	assert.NoError(t, state.Apply(s2, <-watcher))
	<-watcher // consume commit event

	err = s2.View(func(tx2 state.ReadTx) error {
		assert.Nil(t, tx2.Tasks().Get("id1"))
		return nil
	})
	assert.NoError(t, err)
}

func TestFailedTransaction(t *testing.T) {
	s := NewMemoryStore(nil)
	assert.NotNil(t, s)

	// Create one node
	err := s.Update(func(tx state.Tx) error {
		n := &api.Node{
			ID: "id1",
			Spec: &api.NodeSpec{
				Annotations: api.Annotations{
					Name: "name1",
				},
			},
		}

		assert.NoError(t, tx.Nodes().Create(n))
		return nil
	})
	assert.NoError(t, err)

	// Create a second node, but then roll back the transaction
	err = s.Update(func(tx state.Tx) error {
		n := &api.Node{
			ID: "id2",
			Spec: &api.NodeSpec{
				Annotations: api.Annotations{
					Name: "name2",
				},
			},
		}

		assert.NoError(t, tx.Nodes().Create(n))
		return errors.New("rollback")
	})
	assert.Error(t, err)

	err = s.View(func(tx state.ReadTx) error {
		foundNodes, err := tx.Nodes().Find(state.All)
		assert.NoError(t, err)
		assert.Len(t, foundNodes, 1)
		foundNodes, err = tx.Nodes().Find(state.ByName("name1"))
		assert.NoError(t, err)
		assert.Len(t, foundNodes, 1)
		foundNodes, err = tx.Nodes().Find(state.ByName("name2"))
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
			Annotations: api.Annotations{
				Name: "name1",
			},
		},
	}
	err := s.Update(func(tx state.Tx) error {
		assert.NoError(t, tx.Nodes().Create(n))
		return nil
	})
	assert.NoError(t, err)

	// Update the node using an object fetched from the store.
	n.Spec.Annotations.Name = "name2"
	err = s.Update(func(tx state.Tx) error {
		assert.NoError(t, tx.Nodes().Update(n))
		retrievedNode = tx.Nodes().Get(n.ID)
		return nil
	})
	assert.NoError(t, err)

	// Try again, this time using the retrieved node.
	retrievedNode.Spec.Annotations.Name = "name2"
	err = s.Update(func(tx state.Tx) error {
		assert.NoError(t, tx.Nodes().Update(retrievedNode))
		retrievedNode2 = tx.Nodes().Get(n.ID)
		return nil
	})
	assert.NoError(t, err)

	// Try to update retrievedNode again. This should fail because it was
	// already used to perform an update.
	retrievedNode.Spec.Annotations.Name = "name3"
	err = s.Update(func(tx state.Tx) error {
		assert.Equal(t, state.ErrSequenceConflict, tx.Nodes().Update(n))
		return nil
	})
	assert.NoError(t, err)

	// But using retrievedNode2 should work, since it has the latest
	// sequence information.
	retrievedNode2.Spec.Annotations.Name = "name3"
	err = s.Update(func(tx state.Tx) error {
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
	err := s1.View(func(tx state.ReadTx) error {
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

	err = s2.View(func(tx state.ReadTx) error {
		allTasks, err := tx.Tasks().Find(state.All)
		assert.NoError(t, err)
		assert.Len(t, allTasks, len(taskSet))
		for i := range allTasks {
			assert.Equal(t, allTasks[i], taskSet[i])
		}

		allNodes, err := tx.Nodes().Find(state.All)
		assert.NoError(t, err)
		assert.Len(t, allNodes, len(nodeSet))
		for i := range allNodes {
			assert.Equal(t, allNodes[i], nodeSet[i])
		}

		allNetworks, err := tx.Networks().Find(state.All)
		assert.NoError(t, err)
		assert.Len(t, allNetworks, len(networkSet))
		for i := range allNetworks {
			assert.Equal(t, allNetworks[i], networkSet[i])
		}

		allServices, err := tx.Services().Find(state.All)
		assert.NoError(t, err)
		assert.Len(t, allServices, len(serviceSet))
		for i := range allServices {
			assert.Equal(t, allServices[i], serviceSet[i])
		}

		return nil
	})
	assert.NoError(t, err)
}

const benchmarkNumNodes = 10000

func setupNodes(b *testing.B, n int) (state.Store, []string) {
	s := NewMemoryStore(nil)

	nodeIDs := make([]string, n)

	for i := 0; i < n; i++ {
		nodeIDs[i] = identity.NewID()
	}

	b.ResetTimer()

	_ = s.Update(func(tx1 state.Tx) error {
		for i := 0; i < n; i++ {
			_ = tx1.Nodes().Create(&api.Node{
				ID: nodeIDs[i],
				Spec: &api.NodeSpec{
					Annotations: api.Annotations{
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
	_ = s.Update(func(tx1 state.Tx) error {
		for i := 0; i < b.N; i++ {
			_ = tx1.Nodes().Update(&api.Node{
				ID: nodeIDs[i%benchmarkNumNodes],
				Spec: &api.NodeSpec{
					Annotations: api.Annotations{
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
		_ = s.Update(func(tx1 state.Tx) error {
			_ = tx1.Nodes().Update(&api.Node{
				ID: nodeIDs[i%benchmarkNumNodes],
				Spec: &api.NodeSpec{
					Annotations: api.Annotations{
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
		_ = s.Update(func(tx1 state.Tx) error {
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
	_ = s.View(func(tx1 state.ReadTx) error {
		for i := 0; i < b.N; i++ {
			_ = tx1.Nodes().Get(nodeIDs[i%benchmarkNumNodes])
		}
		return nil
	})
}

func BenchmarkFindAllNodes(b *testing.B) {
	s, _ := setupNodes(b, benchmarkNumNodes)
	b.ResetTimer()
	_ = s.View(func(tx1 state.ReadTx) error {
		for i := 0; i < b.N; i++ {
			_, _ = tx1.Nodes().Find(state.All)
		}
		return nil
	})
}

func BenchmarkFindNodeByName(b *testing.B) {
	s, _ := setupNodes(b, benchmarkNumNodes)
	b.ResetTimer()
	_ = s.View(func(tx1 state.ReadTx) error {
		for i := 0; i < b.N; i++ {
			_, _ = tx1.Nodes().Find(state.ByName("name" + strconv.Itoa(i)))
		}
		return nil
	})
}

func BenchmarkFindNodeByQuery(b *testing.B) {
	s, _ := setupNodes(b, benchmarkNumNodes)
	b.ResetTimer()
	_ = s.View(func(tx1 state.ReadTx) error {
		for i := 0; i < b.N; i++ {
			_, _ = tx1.Nodes().Find(state.ByQuery("name" + strconv.Itoa(i)))
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
				_ = s.Update(func(tx1 state.Tx) error {
					_ = tx1.Nodes().Update(&api.Node{
						ID: nodeIDs[i%benchmarkNumNodes],
						Spec: &api.NodeSpec{
							Annotations: api.Annotations{
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
			_ = s.View(func(tx1 state.ReadTx) error {
				for i := 0; i < b.N; i++ {
					_ = tx1.Nodes().Get(nodeIDs[i%benchmarkNumNodes])
				}
				return nil
			})
		}()
	}

	wg.Wait()
}
