package store

import (
	"errors"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/docker/swarmkit/api"
	"github.com/docker/swarmkit/identity"
	"github.com/docker/swarmkit/manager/state"
	"github.com/stretchr/testify/assert"
	"golang.org/x/net/context"
)

var (
	nodeSet = []*api.Node{
		{
			ID: "id1",
			Spec: api.NodeSpec{
				Membership: api.NodeMembershipPending,
			},
			Description: &api.NodeDescription{
				Hostname: "name1",
			},
			Role: api.NodeRoleManager,
		},
		{
			ID: "id2",
			Spec: api.NodeSpec{
				Membership: api.NodeMembershipAccepted,
			},
			Description: &api.NodeDescription{
				Hostname: "name2",
			},
			Role: api.NodeRoleWorker,
		},
		{
			ID: "id3",
			Spec: api.NodeSpec{
				Membership: api.NodeMembershipAccepted,
			},
			Description: &api.NodeDescription{
				// intentionally conflicting hostname
				Hostname: "name2",
			},
			Role: api.NodeRoleWorker,
		},
	}

	serviceSet = []*api.Service{
		{
			ID: "id1",
			Spec: api.ServiceSpec{
				Annotations: api.Annotations{
					Name: "name1",
				},
			},
		},
		{
			ID: "id2",
			Spec: api.ServiceSpec{
				Annotations: api.Annotations{
					Name: "name2",
				},
				Mode: &api.ServiceSpec_Global{
					Global: &api.GlobalService{},
				},
			},
		},
		{
			ID: "id3",
			Spec: api.ServiceSpec{
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
			ServiceAnnotations: api.Annotations{
				Name: "name1",
			},
			DesiredState: api.TaskStateRunning,
			NodeID:       nodeSet[0].ID,
		},
		{
			ID: "id2",
			Annotations: api.Annotations{
				Name: "name2.1",
			},
			ServiceAnnotations: api.Annotations{
				Name: "name2",
			},
			DesiredState: api.TaskStateRunning,
			ServiceID:    serviceSet[0].ID,
		},
		{
			ID: "id3",
			Annotations: api.Annotations{
				Name: "name2.2",
			},
			ServiceAnnotations: api.Annotations{
				Name: "name2",
			},
			DesiredState: api.TaskStateShutdown,
		},
	}

	networkSet = []*api.Network{
		{
			ID: "id1",
			Spec: api.NetworkSpec{
				Annotations: api.Annotations{
					Name: "name1",
				},
			},
		},
		{
			ID: "id2",
			Spec: api.NetworkSpec{
				Annotations: api.Annotations{
					Name: "name2",
				},
			},
		},
		{
			ID: "id3",
			Spec: api.NetworkSpec{
				Annotations: api.Annotations{
					Name: "name3",
				},
			},
		},
	}
)

func setupTestStore(t *testing.T, s *MemoryStore) {
	err := s.Update(func(tx Tx) error {
		// Prepoulate nodes
		for _, n := range nodeSet {
			assert.NoError(t, CreateNode(tx, n))
		}

		// Prepopulate services
		for _, s := range serviceSet {
			assert.NoError(t, CreateService(tx, s))
		}
		// Prepopulate tasks
		for _, task := range taskSet {
			assert.NoError(t, CreateTask(tx, task))
		}
		// Prepopulate networks
		for _, n := range networkSet {
			assert.NoError(t, CreateNetwork(tx, n))
		}

		return nil
	})
	assert.NoError(t, err)
}

func TestStoreNode(t *testing.T) {
	s := NewMemoryStore(nil)
	assert.NotNil(t, s)

	s.View(func(readTx ReadTx) {
		allNodes, err := FindNodes(readTx, All)
		assert.NoError(t, err)
		assert.Empty(t, allNodes)
	})

	setupTestStore(t, s)

	err := s.Update(func(tx Tx) error {
		allNodes, err := FindNodes(tx, All)
		assert.NoError(t, err)
		assert.Len(t, allNodes, len(nodeSet))

		assert.Error(t, CreateNode(tx, nodeSet[0]), "duplicate IDs must be rejected")
		return nil
	})
	assert.NoError(t, err)

	s.View(func(readTx ReadTx) {
		assert.Equal(t, nodeSet[0], GetNode(readTx, "id1"))
		assert.Equal(t, nodeSet[1], GetNode(readTx, "id2"))
		assert.Equal(t, nodeSet[2], GetNode(readTx, "id3"))

		foundNodes, err := FindNodes(readTx, ByName("name1"))
		assert.NoError(t, err)
		assert.Len(t, foundNodes, 1)
		foundNodes, err = FindNodes(readTx, ByName("name2"))
		assert.NoError(t, err)
		assert.Len(t, foundNodes, 2)
		foundNodes, err = FindNodes(readTx, Or(ByName("name1"), ByName("name2")))
		assert.NoError(t, err)
		assert.Len(t, foundNodes, 3)
		foundNodes, err = FindNodes(readTx, ByName("invalid"))
		assert.NoError(t, err)
		assert.Len(t, foundNodes, 0)

		foundNodes, err = FindNodes(readTx, ByIDPrefix("id"))
		assert.NoError(t, err)
		assert.Len(t, foundNodes, 3)

		foundNodes, err = FindNodes(readTx, ByRole(api.NodeRoleManager))
		assert.NoError(t, err)
		assert.Len(t, foundNodes, 1)

		foundNodes, err = FindNodes(readTx, ByRole(api.NodeRoleWorker))
		assert.NoError(t, err)
		assert.Len(t, foundNodes, 2)

		foundNodes, err = FindNodes(readTx, ByMembership(api.NodeMembershipPending))
		assert.NoError(t, err)
		assert.Len(t, foundNodes, 1)

		foundNodes, err = FindNodes(readTx, ByMembership(api.NodeMembershipAccepted))
		assert.NoError(t, err)
		assert.Len(t, foundNodes, 2)
	})

	// Update.
	update := &api.Node{
		ID: "id3",
		Description: &api.NodeDescription{
			Hostname: "name3",
		},
	}
	err = s.Update(func(tx Tx) error {
		assert.NotEqual(t, update, GetNode(tx, "id3"))
		assert.NoError(t, UpdateNode(tx, update))
		assert.Equal(t, update, GetNode(tx, "id3"))

		foundNodes, err := FindNodes(tx, ByName("name2"))
		assert.NoError(t, err)
		assert.Len(t, foundNodes, 1)
		foundNodes, err = FindNodes(tx, ByName("name3"))
		assert.NoError(t, err)
		assert.Len(t, foundNodes, 1)

		invalidUpdate := *nodeSet[0]
		invalidUpdate.ID = "invalid"
		assert.Error(t, UpdateNode(tx, &invalidUpdate), "invalid IDs should be rejected")

		// Delete
		assert.NotNil(t, GetNode(tx, "id1"))
		assert.NoError(t, DeleteNode(tx, "id1"))
		assert.Nil(t, GetNode(tx, "id1"))
		foundNodes, err = FindNodes(tx, ByName("name1"))
		assert.NoError(t, err)
		assert.Empty(t, foundNodes)

		assert.Equal(t, DeleteNode(tx, "nonexistent"), ErrNotExist)
		return nil
	})
	assert.NoError(t, err)
}

func TestStoreService(t *testing.T) {
	s := NewMemoryStore(nil)
	assert.NotNil(t, s)

	s.View(func(readTx ReadTx) {
		allServices, err := FindServices(readTx, All)
		assert.NoError(t, err)
		assert.Empty(t, allServices)
	})

	setupTestStore(t, s)

	err := s.Update(func(tx Tx) error {
		assert.Equal(t,
			CreateService(tx, &api.Service{
				ID: "id1",
				Spec: api.ServiceSpec{
					Annotations: api.Annotations{
						Name: "name4",
					},
				},
			}), ErrExist, "duplicate IDs must be rejected")

		assert.Equal(t,
			CreateService(tx, &api.Service{
				ID: "id4",
				Spec: api.ServiceSpec{
					Annotations: api.Annotations{
						Name: "name1",
					},
				},
			}), ErrNameConflict, "duplicate names must be rejected")

		assert.Equal(t,
			CreateService(tx, &api.Service{
				ID: "id4",
				Spec: api.ServiceSpec{
					Annotations: api.Annotations{
						Name: "NAME1",
					},
				},
			}), ErrNameConflict, "duplicate check should be case insensitive")
		return nil
	})
	assert.NoError(t, err)

	s.View(func(readTx ReadTx) {
		assert.Equal(t, serviceSet[0], GetService(readTx, "id1"))
		assert.Equal(t, serviceSet[1], GetService(readTx, "id2"))
		assert.Equal(t, serviceSet[2], GetService(readTx, "id3"))

		foundServices, err := FindServices(readTx, ByNamePrefix("name1"))
		assert.NoError(t, err)
		assert.Len(t, foundServices, 1)
		foundServices, err = FindServices(readTx, ByNamePrefix("NAME1"))
		assert.NoError(t, err)
		assert.Len(t, foundServices, 1)
		foundServices, err = FindServices(readTx, ByNamePrefix("invalid"))
		assert.NoError(t, err)
		assert.Len(t, foundServices, 0)
		foundServices, err = FindServices(readTx, Or(ByNamePrefix("name1"), ByNamePrefix("name2")))
		assert.NoError(t, err)
		assert.Len(t, foundServices, 2)
		foundServices, err = FindServices(readTx, Or(ByNamePrefix("name1"), ByNamePrefix("name2"), ByNamePrefix("name4")))
		assert.NoError(t, err)
		assert.Len(t, foundServices, 2)

		foundServices, err = FindServices(readTx, ByIDPrefix("id"))
		assert.NoError(t, err)
		assert.Len(t, foundServices, 3)
	})

	// Update.
	err = s.Update(func(tx Tx) error {
		// Regular update.
		update := serviceSet[0].Copy()
		update.Spec.Annotations.Labels = map[string]string{
			"foo": "bar",
		}

		assert.NotEqual(t, update, GetService(tx, update.ID))
		assert.NoError(t, UpdateService(tx, update))
		assert.Equal(t, update, GetService(tx, update.ID))

		// Name conflict.
		update = GetService(tx, update.ID)
		update.Spec.Annotations.Name = "name2"
		assert.Equal(t, UpdateService(tx, update), ErrNameConflict, "duplicate names should be rejected")
		update = GetService(tx, update.ID)
		update.Spec.Annotations.Name = "NAME2"
		assert.Equal(t, UpdateService(tx, update), ErrNameConflict, "duplicate check should be case insensitive")

		// Name change.
		update = GetService(tx, update.ID)
		foundServices, err := FindServices(tx, ByNamePrefix("name1"))
		assert.NoError(t, err)
		assert.Len(t, foundServices, 1)
		foundServices, err = FindServices(tx, ByNamePrefix("name4"))
		assert.NoError(t, err)
		assert.Empty(t, foundServices)

		update.Spec.Annotations.Name = "name4"
		assert.NoError(t, UpdateService(tx, update))
		foundServices, err = FindServices(tx, ByNamePrefix("name1"))
		assert.NoError(t, err)
		assert.Empty(t, foundServices)
		foundServices, err = FindServices(tx, ByNamePrefix("name4"))
		assert.NoError(t, err)
		assert.Len(t, foundServices, 1)

		// Invalid update.
		invalidUpdate := serviceSet[0].Copy()
		invalidUpdate.ID = "invalid"
		assert.Error(t, UpdateService(tx, invalidUpdate), "invalid IDs should be rejected")

		return nil
	})
	assert.NoError(t, err)

	// Delete
	err = s.Update(func(tx Tx) error {
		assert.NotNil(t, GetService(tx, "id1"))
		assert.NoError(t, DeleteService(tx, "id1"))
		assert.Nil(t, GetService(tx, "id1"))
		foundServices, err := FindServices(tx, ByNamePrefix("name1"))
		assert.NoError(t, err)
		assert.Empty(t, foundServices)

		assert.Equal(t, DeleteService(tx, "nonexistent"), ErrNotExist)
		return nil
	})
	assert.NoError(t, err)
}

func TestStoreNetwork(t *testing.T) {
	s := NewMemoryStore(nil)
	assert.NotNil(t, s)

	s.View(func(readTx ReadTx) {
		allNetworks, err := FindNetworks(readTx, All)
		assert.NoError(t, err)
		assert.Empty(t, allNetworks)
	})

	setupTestStore(t, s)

	err := s.Update(func(tx Tx) error {
		allNetworks, err := FindNetworks(tx, All)
		assert.NoError(t, err)
		assert.Len(t, allNetworks, len(networkSet))

		assert.Error(t, CreateNetwork(tx, networkSet[0]), "duplicate IDs must be rejected")
		return nil
	})
	assert.NoError(t, err)

	s.View(func(readTx ReadTx) {
		assert.Equal(t, networkSet[0], GetNetwork(readTx, "id1"))
		assert.Equal(t, networkSet[1], GetNetwork(readTx, "id2"))
		assert.Equal(t, networkSet[2], GetNetwork(readTx, "id3"))

		foundNetworks, err := FindNetworks(readTx, ByName("name1"))
		assert.NoError(t, err)
		assert.Len(t, foundNetworks, 1)
		foundNetworks, err = FindNetworks(readTx, ByName("name2"))
		assert.NoError(t, err)
		assert.Len(t, foundNetworks, 1)
		foundNetworks, err = FindNetworks(readTx, ByName("invalid"))
		assert.NoError(t, err)
		assert.Len(t, foundNetworks, 0)
	})

	err = s.Update(func(tx Tx) error {
		// Delete
		assert.NotNil(t, GetNetwork(tx, "id1"))
		assert.NoError(t, DeleteNetwork(tx, "id1"))
		assert.Nil(t, GetNetwork(tx, "id1"))
		foundNetworks, err := FindNetworks(tx, ByName("name1"))
		assert.NoError(t, err)
		assert.Empty(t, foundNetworks)

		assert.Equal(t, DeleteNetwork(tx, "nonexistent"), ErrNotExist)
		return nil
	})

	assert.NoError(t, err)
}

func TestStoreTask(t *testing.T) {
	s := NewMemoryStore(nil)
	assert.NotNil(t, s)

	s.View(func(tx ReadTx) {
		allTasks, err := FindTasks(tx, All)
		assert.NoError(t, err)
		assert.Empty(t, allTasks)
	})

	setupTestStore(t, s)

	err := s.Update(func(tx Tx) error {
		allTasks, err := FindTasks(tx, All)
		assert.NoError(t, err)
		assert.Len(t, allTasks, len(taskSet))

		assert.Error(t, CreateTask(tx, taskSet[0]), "duplicate IDs must be rejected")
		return nil
	})
	assert.NoError(t, err)

	s.View(func(readTx ReadTx) {
		assert.Equal(t, taskSet[0], GetTask(readTx, "id1"))
		assert.Equal(t, taskSet[1], GetTask(readTx, "id2"))
		assert.Equal(t, taskSet[2], GetTask(readTx, "id3"))

		foundTasks, err := FindTasks(readTx, ByNamePrefix("name1"))
		assert.NoError(t, err)
		assert.Len(t, foundTasks, 1)
		foundTasks, err = FindTasks(readTx, ByNamePrefix("name2"))
		assert.NoError(t, err)
		assert.Len(t, foundTasks, 2)
		foundTasks, err = FindTasks(readTx, ByNamePrefix("invalid"))
		assert.NoError(t, err)
		assert.Len(t, foundTasks, 0)

		foundTasks, err = FindTasks(readTx, ByNodeID(nodeSet[0].ID))
		assert.NoError(t, err)
		assert.Len(t, foundTasks, 1)
		assert.Equal(t, foundTasks[0], taskSet[0])
		foundTasks, err = FindTasks(readTx, ByNodeID("invalid"))
		assert.NoError(t, err)
		assert.Len(t, foundTasks, 0)

		foundTasks, err = FindTasks(readTx, ByServiceID(serviceSet[0].ID))
		assert.NoError(t, err)
		assert.Len(t, foundTasks, 1)
		assert.Equal(t, foundTasks[0], taskSet[1])
		foundTasks, err = FindTasks(readTx, ByServiceID("invalid"))
		assert.NoError(t, err)
		assert.Len(t, foundTasks, 0)

		foundTasks, err = FindTasks(readTx, ByDesiredState(api.TaskStateRunning))
		assert.NoError(t, err)
		assert.Len(t, foundTasks, 2)
		assert.Equal(t, foundTasks[0].DesiredState, api.TaskStateRunning)
		assert.Equal(t, foundTasks[0].DesiredState, api.TaskStateRunning)
		foundTasks, err = FindTasks(readTx, ByDesiredState(api.TaskStateShutdown))
		assert.NoError(t, err)
		assert.Len(t, foundTasks, 1)
		assert.Equal(t, foundTasks[0], taskSet[2])
		foundTasks, err = FindTasks(readTx, ByDesiredState(api.TaskStatePending))
		assert.NoError(t, err)
		assert.Len(t, foundTasks, 0)
	})

	// Update.
	update := &api.Task{
		ID: "id3",
		Annotations: api.Annotations{
			Name: "name3",
		},
		ServiceAnnotations: api.Annotations{
			Name: "name3",
		},
	}
	err = s.Update(func(tx Tx) error {
		assert.NotEqual(t, update, GetTask(tx, "id3"))
		assert.NoError(t, UpdateTask(tx, update))
		assert.Equal(t, update, GetTask(tx, "id3"))

		foundTasks, err := FindTasks(tx, ByNamePrefix("name2"))
		assert.NoError(t, err)
		assert.Len(t, foundTasks, 1)
		foundTasks, err = FindTasks(tx, ByNamePrefix("name3"))
		assert.NoError(t, err)
		assert.Len(t, foundTasks, 1)

		invalidUpdate := *taskSet[0]
		invalidUpdate.ID = "invalid"
		assert.Error(t, UpdateTask(tx, &invalidUpdate), "invalid IDs should be rejected")

		// Delete
		assert.NotNil(t, GetTask(tx, "id1"))
		assert.NoError(t, DeleteTask(tx, "id1"))
		assert.Nil(t, GetTask(tx, "id1"))
		foundTasks, err = FindTasks(tx, ByNamePrefix("name1"))
		assert.NoError(t, err)
		assert.Empty(t, foundTasks)

		assert.Equal(t, DeleteTask(tx, "nonexistent"), ErrNotExist)
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

	copyToS2 := func(readTx ReadTx) error {
		return s2.Update(func(tx Tx) error {
			// Copy over new data
			nodes, err := FindNodes(readTx, All)
			if err != nil {
				return err
			}
			for _, n := range nodes {
				if err := CreateNode(tx, n); err != nil {
					return err
				}
			}

			tasks, err := FindTasks(readTx, All)
			if err != nil {
				return err
			}
			for _, t := range tasks {
				if err := CreateTask(tx, t); err != nil {
					return err
				}
			}

			services, err := FindServices(readTx, All)
			if err != nil {
				return err
			}
			for _, s := range services {
				if err := CreateService(tx, s); err != nil {
					return err
				}
			}

			networks, err := FindNetworks(readTx, All)
			if err != nil {
				return err
			}
			for _, n := range networks {
				if err := CreateNetwork(tx, n); err != nil {
					return err
				}
			}

			return nil
		})
	}

	// Fork
	watcher, cancel, err := ViewAndWatch(s1, copyToS2)
	defer cancel()
	assert.NoError(t, err)

	s2.View(func(tx2 ReadTx) {
		assert.Equal(t, nodeSet[0], GetNode(tx2, "id1"))
		assert.Equal(t, nodeSet[1], GetNode(tx2, "id2"))
		assert.Equal(t, nodeSet[2], GetNode(tx2, "id3"))

		assert.Equal(t, serviceSet[0], GetService(tx2, "id1"))
		assert.Equal(t, serviceSet[1], GetService(tx2, "id2"))
		assert.Equal(t, serviceSet[2], GetService(tx2, "id3"))

		assert.Equal(t, taskSet[0], GetTask(tx2, "id1"))
		assert.Equal(t, taskSet[1], GetTask(tx2, "id2"))
		assert.Equal(t, taskSet[2], GetTask(tx2, "id3"))
	})

	// Create node
	createNode := &api.Node{
		ID: "id4",
		Spec: api.NodeSpec{
			Annotations: api.Annotations{
				Name: "name4",
			},
		},
	}

	err = s1.Update(func(tx1 Tx) error {
		assert.NoError(t, CreateNode(tx1, createNode))
		return nil
	})
	assert.NoError(t, err)

	assert.NoError(t, Apply(s2, <-watcher))
	<-watcher // consume commit event

	s2.View(func(tx2 ReadTx) {
		assert.Equal(t, createNode, GetNode(tx2, "id4"))
	})

	// Update node
	updateNode := &api.Node{
		ID: "id3",
		Spec: api.NodeSpec{
			Annotations: api.Annotations{
				Name: "name3",
			},
		},
	}

	err = s1.Update(func(tx1 Tx) error {
		assert.NoError(t, UpdateNode(tx1, updateNode))
		return nil
	})
	assert.NoError(t, err)

	assert.NoError(t, Apply(s2, <-watcher))
	<-watcher // consume commit event

	s2.View(func(tx2 ReadTx) {
		assert.Equal(t, updateNode, GetNode(tx2, "id3"))
	})

	err = s1.Update(func(tx1 Tx) error {
		// Delete node
		assert.NoError(t, DeleteNode(tx1, "id1"))
		return nil
	})
	assert.NoError(t, err)

	assert.NoError(t, Apply(s2, <-watcher))
	<-watcher // consume commit event

	s2.View(func(tx2 ReadTx) {
		assert.Nil(t, GetNode(tx2, "id1"))
	})

	// Create service
	createService := &api.Service{
		ID: "id4",
		Spec: api.ServiceSpec{
			Annotations: api.Annotations{
				Name: "name4",
			},
		},
	}

	err = s1.Update(func(tx1 Tx) error {
		assert.NoError(t, CreateService(tx1, createService))
		return nil
	})
	assert.NoError(t, err)

	assert.NoError(t, Apply(s2, <-watcher))
	<-watcher // consume commit event

	s2.View(func(tx2 ReadTx) {
		assert.Equal(t, createService, GetService(tx2, "id4"))
	})

	// Update service
	updateService := serviceSet[2].Copy()
	updateService.Spec.Annotations.Name = "new-name"
	err = s1.Update(func(tx1 Tx) error {
		assert.NotEqual(t, updateService, GetService(tx1, updateService.ID))
		assert.NoError(t, UpdateService(tx1, updateService))
		return nil
	})
	assert.NoError(t, err)

	assert.NoError(t, Apply(s2, <-watcher))
	<-watcher // consume commit event

	s2.View(func(tx2 ReadTx) {
		assert.Equal(t, updateService, GetService(tx2, "id3"))
	})

	err = s1.Update(func(tx1 Tx) error {
		// Delete service
		assert.NoError(t, DeleteService(tx1, "id1"))
		return nil
	})
	assert.NoError(t, err)

	assert.NoError(t, Apply(s2, <-watcher))
	<-watcher // consume commit event

	s2.View(func(tx2 ReadTx) {
		assert.Nil(t, GetService(tx2, "id1"))
	})

	// Create task
	createTask := &api.Task{
		ID: "id4",
		ServiceAnnotations: api.Annotations{
			Name: "name4",
		},
	}

	err = s1.Update(func(tx1 Tx) error {
		assert.NoError(t, CreateTask(tx1, createTask))
		return nil
	})
	assert.NoError(t, err)

	assert.NoError(t, Apply(s2, <-watcher))
	<-watcher // consume commit event

	s2.View(func(tx2 ReadTx) {
		assert.Equal(t, createTask, GetTask(tx2, "id4"))
	})

	// Update task
	updateTask := &api.Task{
		ID: "id3",
		ServiceAnnotations: api.Annotations{
			Name: "name3",
		},
	}

	err = s1.Update(func(tx1 Tx) error {
		assert.NoError(t, UpdateTask(tx1, updateTask))
		return nil
	})
	assert.NoError(t, err)
	assert.NoError(t, Apply(s2, <-watcher))
	<-watcher // consume commit event

	s2.View(func(tx2 ReadTx) {
		assert.Equal(t, updateTask, GetTask(tx2, "id3"))
	})

	err = s1.Update(func(tx1 Tx) error {
		// Delete task
		assert.NoError(t, DeleteTask(tx1, "id1"))
		return nil
	})
	assert.NoError(t, err)
	assert.NoError(t, Apply(s2, <-watcher))
	<-watcher // consume commit event

	s2.View(func(tx2 ReadTx) {
		assert.Nil(t, GetTask(tx2, "id1"))
	})
}

func TestFailedTransaction(t *testing.T) {
	s := NewMemoryStore(nil)
	assert.NotNil(t, s)

	// Create one node
	err := s.Update(func(tx Tx) error {
		n := &api.Node{
			ID: "id1",
			Description: &api.NodeDescription{
				Hostname: "name1",
			},
		}

		assert.NoError(t, CreateNode(tx, n))
		return nil
	})
	assert.NoError(t, err)

	// Create a second node, but then roll back the transaction
	err = s.Update(func(tx Tx) error {
		n := &api.Node{
			ID: "id2",
			Description: &api.NodeDescription{
				Hostname: "name2",
			},
		}

		assert.NoError(t, CreateNode(tx, n))
		return errors.New("rollback")
	})
	assert.Error(t, err)

	s.View(func(tx ReadTx) {
		foundNodes, err := FindNodes(tx, All)
		assert.NoError(t, err)
		assert.Len(t, foundNodes, 1)
		foundNodes, err = FindNodes(tx, ByName("name1"))
		assert.NoError(t, err)
		assert.Len(t, foundNodes, 1)
		foundNodes, err = FindNodes(tx, ByName("name2"))
		assert.NoError(t, err)
		assert.Len(t, foundNodes, 0)
	})
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
		Spec: api.NodeSpec{
			Annotations: api.Annotations{
				Name: "name1",
			},
		},
	}
	err := s.Update(func(tx Tx) error {
		assert.NoError(t, CreateNode(tx, n))
		return nil
	})
	assert.NoError(t, err)

	// Update the node using an object fetched from the store.
	n.Spec.Annotations.Name = "name2"
	err = s.Update(func(tx Tx) error {
		assert.NoError(t, UpdateNode(tx, n))
		retrievedNode = GetNode(tx, n.ID)
		return nil
	})
	assert.NoError(t, err)

	// Make sure the store is updating our local copy with the version.
	assert.Equal(t, n.Meta.Version, retrievedNode.Meta.Version)

	// Try again, this time using the retrieved node.
	retrievedNode.Spec.Annotations.Name = "name2"
	err = s.Update(func(tx Tx) error {
		assert.NoError(t, UpdateNode(tx, retrievedNode))
		retrievedNode2 = GetNode(tx, n.ID)
		return nil
	})
	assert.NoError(t, err)

	// Try to update retrievedNode again. This should fail because it was
	// already used to perform an update.
	retrievedNode.Spec.Annotations.Name = "name3"
	err = s.Update(func(tx Tx) error {
		assert.Equal(t, ErrSequenceConflict, UpdateNode(tx, n))
		return nil
	})
	assert.NoError(t, err)

	// But using retrievedNode2 should work, since it has the latest
	// sequence information.
	retrievedNode2.Spec.Annotations.Name = "name3"
	err = s.Update(func(tx Tx) error {
		assert.NoError(t, UpdateNode(tx, retrievedNode2))
		return nil
	})
	assert.NoError(t, err)
}

func TestTimestamps(t *testing.T) {
	var mockProposer mockProposer
	s := NewMemoryStore(&mockProposer)
	assert.NotNil(t, s)

	var (
		retrievedNode *api.Node
		updatedNode   *api.Node
	)

	// Create one node
	n := &api.Node{
		ID: "id1",
		Spec: api.NodeSpec{
			Annotations: api.Annotations{
				Name: "name1",
			},
		},
	}
	err := s.Update(func(tx Tx) error {
		assert.NoError(t, CreateNode(tx, n))
		return nil
	})
	assert.NoError(t, err)

	// Make sure our local copy got updated.
	assert.NotZero(t, n.Meta.CreatedAt)
	assert.NotZero(t, n.Meta.UpdatedAt)
	// Since this is a new node, CreatedAt should equal UpdatedAt.
	assert.Equal(t, n.Meta.CreatedAt, n.Meta.UpdatedAt)

	// Fetch the node from the store and make sure timestamps match.
	s.View(func(tx ReadTx) {
		retrievedNode = GetNode(tx, n.ID)
	})
	assert.Equal(t, retrievedNode.Meta.CreatedAt, n.Meta.CreatedAt)
	assert.Equal(t, retrievedNode.Meta.UpdatedAt, n.Meta.UpdatedAt)

	// Make an update.
	retrievedNode.Spec.Annotations.Name = "name2"
	err = s.Update(func(tx Tx) error {
		assert.NoError(t, UpdateNode(tx, retrievedNode))
		updatedNode = GetNode(tx, n.ID)
		return nil
	})
	assert.NoError(t, err)

	// Ensure `CreatedAt` is the same after the update and `UpdatedAt` got updated.
	assert.Equal(t, updatedNode.Meta.CreatedAt, n.Meta.CreatedAt)
	assert.NotEqual(t, updatedNode.Meta.CreatedAt, updatedNode.Meta.UpdatedAt)
}

func TestBatch(t *testing.T) {
	var mockProposer mockProposer
	s := NewMemoryStore(&mockProposer)
	assert.NotNil(t, s)

	watch, cancel := s.WatchQueue().Watch()
	defer cancel()

	// Create 405 nodes. Should get split across 3 transactions.
	committed, err := s.Batch(func(batch *Batch) error {
		for i := 0; i != 2*MaxChangesPerTransaction+5; i++ {
			n := &api.Node{
				ID: "id" + strconv.Itoa(i),
				Spec: api.NodeSpec{
					Annotations: api.Annotations{
						Name: "name" + strconv.Itoa(i),
					},
				},
			}

			batch.Update(func(tx Tx) error {
				assert.NoError(t, CreateNode(tx, n))
				return nil
			})
		}

		return nil
	})
	assert.NoError(t, err)
	assert.Equal(t, 2*MaxChangesPerTransaction+5, committed)

	for i := 0; i != MaxChangesPerTransaction; i++ {
		event := <-watch
		if _, ok := event.(state.EventCreateNode); !ok {
			t.Fatalf("expected EventCreateNode; got %#v", event)
		}
	}
	event := <-watch
	if _, ok := event.(state.EventCommit); !ok {
		t.Fatalf("expected EventCommit; got %#v", event)
	}
	for i := 0; i != MaxChangesPerTransaction; i++ {
		event := <-watch
		if _, ok := event.(state.EventCreateNode); !ok {
			t.Fatalf("expected EventCreateNode; got %#v", event)
		}
	}
	event = <-watch
	if _, ok := event.(state.EventCommit); !ok {
		t.Fatalf("expected EventCommit; got %#v", event)
	}
	for i := 0; i != 5; i++ {
		event := <-watch
		if _, ok := event.(state.EventCreateNode); !ok {
			t.Fatalf("expected EventCreateNode; got %#v", event)
		}
	}
	event = <-watch
	if _, ok := event.(state.EventCommit); !ok {
		t.Fatalf("expected EventCommit; got %#v", event)
	}
}

func TestBatchFailure(t *testing.T) {
	var mockProposer mockProposer
	s := NewMemoryStore(&mockProposer)
	assert.NotNil(t, s)

	watch, cancel := s.WatchQueue().Watch()
	defer cancel()

	// Return an error partway through a transaction.
	committed, err := s.Batch(func(batch *Batch) error {
		for i := 0; ; i++ {
			n := &api.Node{
				ID: "id" + strconv.Itoa(i),
				Spec: api.NodeSpec{
					Annotations: api.Annotations{
						Name: "name" + strconv.Itoa(i),
					},
				},
			}

			batch.Update(func(tx Tx) error {
				assert.NoError(t, CreateNode(tx, n))
				return nil
			})
			if i == MaxChangesPerTransaction+8 {
				return errors.New("failing the current tx")
			}
		}
	})
	assert.Error(t, err)
	assert.Equal(t, MaxChangesPerTransaction, committed)

	for i := 0; i != MaxChangesPerTransaction; i++ {
		event := <-watch
		if _, ok := event.(state.EventCreateNode); !ok {
			t.Fatalf("expected EventCreateNode; got %#v", event)
		}
	}
	event := <-watch
	if _, ok := event.(state.EventCommit); !ok {
		t.Fatalf("expected EventCommit; got %#v", event)
	}

	// Shouldn't be anything after the first transaction
	select {
	case <-watch:
		t.Fatalf("unexpected additional events")
	case <-time.After(50 * time.Millisecond):
	}
}

func TestStoreSaveRestore(t *testing.T) {
	s1 := NewMemoryStore(nil)
	assert.NotNil(t, s1)

	setupTestStore(t, s1)

	var snapshot *api.StoreSnapshot
	s1.View(func(tx ReadTx) {
		var err error
		snapshot, err = s1.Save(tx)
		assert.NoError(t, err)
	})

	s2 := NewMemoryStore(nil)
	assert.NotNil(t, s2)

	err := s2.Restore(snapshot)
	assert.NoError(t, err)

	s2.View(func(tx ReadTx) {
		allTasks, err := FindTasks(tx, All)
		assert.NoError(t, err)
		assert.Len(t, allTasks, len(taskSet))
		for i := range allTasks {
			assert.Equal(t, allTasks[i], taskSet[i])
		}

		allNodes, err := FindNodes(tx, All)
		assert.NoError(t, err)
		assert.Len(t, allNodes, len(nodeSet))
		for i := range allNodes {
			assert.Equal(t, allNodes[i], nodeSet[i])
		}

		allNetworks, err := FindNetworks(tx, All)
		assert.NoError(t, err)
		assert.Len(t, allNetworks, len(networkSet))
		for i := range allNetworks {
			assert.Equal(t, allNetworks[i], networkSet[i])
		}

		allServices, err := FindServices(tx, All)
		assert.NoError(t, err)
		assert.Len(t, allServices, len(serviceSet))
		for i := range allServices {
			assert.Equal(t, allServices[i], serviceSet[i])
		}
	})
}

const benchmarkNumNodes = 10000

func setupNodes(b *testing.B, n int) (*MemoryStore, []string) {
	s := NewMemoryStore(nil)

	nodeIDs := make([]string, n)

	for i := 0; i < n; i++ {
		nodeIDs[i] = identity.NewID()
	}

	b.ResetTimer()

	_ = s.Update(func(tx1 Tx) error {
		for i := 0; i < n; i++ {
			_ = CreateNode(tx1, &api.Node{
				ID: nodeIDs[i],
				Spec: api.NodeSpec{
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
	_ = s.Update(func(tx1 Tx) error {
		for i := 0; i < b.N; i++ {
			_ = UpdateNode(tx1, &api.Node{
				ID: nodeIDs[i%benchmarkNumNodes],
				Spec: api.NodeSpec{
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
		_ = s.Update(func(tx1 Tx) error {
			_ = UpdateNode(tx1, &api.Node{
				ID: nodeIDs[i%benchmarkNumNodes],
				Spec: api.NodeSpec{
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
		_ = s.Update(func(tx1 Tx) error {
			_ = DeleteNode(tx1, nodeIDs[0])
			// Don't actually commit deletions, so we can delete
			// things repeatedly to satisfy the benchmark structure.
			return errors.New("don't commit this")
		})
	}
}

func BenchmarkGetNode(b *testing.B) {
	s, nodeIDs := setupNodes(b, benchmarkNumNodes)
	b.ResetTimer()
	s.View(func(tx1 ReadTx) {
		for i := 0; i < b.N; i++ {
			_ = GetNode(tx1, nodeIDs[i%benchmarkNumNodes])
		}
	})
}

func BenchmarkFindAllNodes(b *testing.B) {
	s, _ := setupNodes(b, benchmarkNumNodes)
	b.ResetTimer()
	s.View(func(tx1 ReadTx) {
		for i := 0; i < b.N; i++ {
			_, _ = FindNodes(tx1, All)
		}
	})
}

func BenchmarkFindNodeByName(b *testing.B) {
	s, _ := setupNodes(b, benchmarkNumNodes)
	b.ResetTimer()
	s.View(func(tx1 ReadTx) {
		for i := 0; i < b.N; i++ {
			_, _ = FindNodes(tx1, ByName("name"+strconv.Itoa(i)))
		}
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
					_ = UpdateNode(tx1, &api.Node{
						ID: nodeIDs[i%benchmarkNumNodes],
						Spec: api.NodeSpec{
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
			s.View(func(tx1 ReadTx) {
				for i := 0; i < b.N; i++ {
					_ = GetNode(tx1, nodeIDs[i%benchmarkNumNodes])
				}
			})
		}()
	}

	wg.Wait()
}
