package deallocator

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/moby/swarmkit/v2/api"
	"github.com/moby/swarmkit/v2/manager/state/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDeallocatorInit(t *testing.T) {
	// start up the memory store
	s := store.NewMemoryStore(nil)
	require.NotNil(t, s)
	defer s.Close()

	// create a service that's pending deletion, with no tasks remaining
	// this one should be deleted by the deallocator
	// additionally, that service is using a network that's also marked for
	// deletion, and another that's not
	network1 := newNetwork("network1", true)
	network2 := newNetwork("network2", false)
	service1 := newService("service1", true, network1, network2)

	// now let's create another service that's also pending deletion, but still
	// has one task associated with it (in any state) - and also uses a network
	// that's also marked for deletion
	// none of those should get deleted
	network3 := newNetwork("network3", true)
	service2 := newService("service2", true, network3)
	task1 := newTask("task1", service2)

	// let's also have a network that's pending deletion,
	// but isn't used by any existing service
	// this one should be gone after the init
	network4 := newNetwork("network4", true)

	// and finally a network that's not pending deletion, not
	// used by any service
	network5 := newNetwork("network5", false)

	createDBObjects(t, s, service1, service2,
		network1, network2, network3, network4, network5, task1)

	// create and start the deallocator
	deallocator, ran := startNewDeallocator(t, s, true)

	// and then stop it immediately - we're just interested in the init
	// phase for this test
	stopDeallocator(t, deallocator, ran)

	// now let's check that the DB is in the state we expect
	s.View(func(tx store.ReadTx) {
		assert.Nil(t, store.GetService(tx, service1.ID))
		assert.Nil(t, store.GetNetwork(tx, network1.ID))
		assert.NotNil(t, store.GetNetwork(tx, network2.ID))

		assert.NotNil(t, store.GetService(tx, service2.ID))
		assert.NotNil(t, store.GetNetwork(tx, network3.ID))

		assert.Nil(t, store.GetNetwork(tx, network4.ID))

		assert.NotNil(t, store.GetNetwork(tx, network5.ID))
	})
}

// this tests what happens when a service is marked for deletion
func TestServiceDelete(t *testing.T) {
	// we test services with respectively 1, 2, 5 and 10 tasks
	for _, taskCount := range []int{1, 2, 5, 10} {
		t.Run("service delete with "+strconv.Itoa(taskCount)+" tasks",
			func(t *testing.T) {
				// start up the memory store
				s := store.NewMemoryStore(nil)
				require.NotNil(t, s)
				defer s.Close()

				// let's create the task and services
				service := newService("service", false)
				createDBObjects(t, s, service)

				taskIDs := make([]string, taskCount)
				tasks := make([]interface{}, taskCount)
				for i := 0; i < taskCount; i++ {
					taskIDs[i] = "task" + strconv.Itoa(i+1)
					tasks[i] = newTask(taskIDs[i], service)
				}
				createDBObjects(t, s, tasks...)

				// now let's start the deallocator
				deallocator, ran := startNewDeallocator(t, s, false)
				defer stopDeallocator(t, deallocator, ran)

				// then let's mark the service for deletion...
				updateStoreAndWaitForEvent(t, deallocator, func(tx store.Tx) {
					service.PendingDelete = true
					require.NoError(t, store.UpdateService(tx, service))
				}, false)

				// and then let's remove all tasks
				for i, taskID := range taskIDs {
					lastTask := i == len(taskIDs)-1

					updateStoreAndWaitForEvent(t, deallocator, func(tx store.Tx) {
						require.NoError(t, store.DeleteTask(tx, taskID))
					}, lastTask)

					// and after each but the last one, the service should still
					// be there - after the last one it should be gone
					s.View(func(tx store.ReadTx) {
						if lastTask {
							require.Nil(t, store.GetService(tx, service.ID))
						} else {
							require.NotNil(t, store.GetService(tx, service.ID))
						}
					})

				}
			})
	}
}

// this tests what happens when a service is marked for deletion,
// along with its network, _before_ the service has had time to
// fully shut down
func TestServiceAndNetworkDelete(t *testing.T) {
	s := store.NewMemoryStore(nil)
	require.NotNil(t, s)
	defer s.Close()

	// let's create a couple of networks, a service, and a couple of tasks
	network1 := newNetwork("network1", false)
	network2 := newNetwork("network2", false)
	service := newService("service", false, network1, network2)
	task1 := newTask("task1", service)
	task2 := newTask("task2", service)

	createDBObjects(t, s, network1, network2, service, task1, task2)

	deallocator, ran := startNewDeallocator(t, s, false)
	defer stopDeallocator(t, deallocator, ran)

	// then let's mark the service and network2 for deletion
	updateStoreAndWaitForEvent(t, deallocator, func(tx store.Tx) {
		service.PendingDelete = true
		require.NoError(t, store.UpdateService(tx, service))
	}, false)

	updateStoreAndWaitForEvent(t, deallocator, func(tx store.Tx) {
		network2.PendingDelete = true
		require.NoError(t, store.UpdateNetwork(tx, network2))
	}, false)

	// then let's delete one task
	updateStoreAndWaitForEvent(t, deallocator, func(tx store.Tx) {
		require.NoError(t, store.DeleteTask(tx, task2.ID))
	}, false)

	// the service and network2 should still exist
	s.View(func(tx store.ReadTx) {
		require.NotNil(t, store.GetService(tx, service.ID))
		require.NotNil(t, store.GetNetwork(tx, network2.ID))
	})

	// now let's delete the other task
	updateStoreAndWaitForEvent(t, deallocator, func(tx store.Tx) {
		require.NoError(t, store.DeleteTask(tx, task1.ID))
	}, true)

	// now the service and network2 should be gone
	s.View(func(tx store.ReadTx) {
		require.Nil(t, store.GetService(tx, service.ID))
		require.Nil(t, store.GetNetwork(tx, network2.ID))

		// quick sanity check, the first service should be
		// unaffected
		require.NotNil(t, store.GetNetwork(tx, network1.ID))
	})
}

// this tests that an update to a service that is _not_ marked it for deletion
// doesn't do anything
func TestServiceNotMarkedForDeletion(t *testing.T) {
	s := store.NewMemoryStore(nil)
	require.NotNil(t, s)
	defer s.Close()

	service := newService("service", false)
	createDBObjects(t, s, service)

	deallocator, ran := startNewDeallocator(t, s, false)
	defer stopDeallocator(t, deallocator, ran)

	updateStoreAndWaitForEvent(t, deallocator, func(tx store.Tx) {
		service.Meta = api.Meta{Version: api.Version{Index: 12}}
		require.NoError(t, store.UpdateService(tx, service))
	},
		// the deallocator shouldn't do any DB updates based on this event
		false)
}

// this tests that an update to a network that is _not_ marked it for deletion
// doesn't do anything
func TestNetworkNotMarkedForDeletion(t *testing.T) {
	s := store.NewMemoryStore(nil)
	require.NotNil(t, s)
	defer s.Close()

	network := newNetwork("network", false)
	service := newService("service", false, network)
	createDBObjects(t, s, network, service)

	deallocator, ran := startNewDeallocator(t, s, false)
	defer stopDeallocator(t, deallocator, ran)

	updateStoreAndWaitForEvent(t, deallocator, func(tx store.Tx) {
		network.IPAM = &api.IPAMOptions{Driver: &api.Driver{Name: "test_driver"}}
		require.NoError(t, store.UpdateNetwork(tx, network))
	},
		// the deallocator shouldn't do any DB updates based on this event
		false)
}

// this test that the deallocator also works with the "old" style of storing
// networks directly on the  service spec (instead of the task spec)
// TODO: as said in the source file, we should really add a helper
// on services objects itself, and test it there instead
func TestDeallocatorWithOldStyleNetworks(t *testing.T) {
	s := store.NewMemoryStore(nil)
	require.NotNil(t, s)
	defer s.Close()

	service := newService("service", true)

	// add a couple of networks with the old style
	network1 := newNetwork("network1", true)
	network2 := newNetwork("network2", false)
	service.Spec.Networks = append(service.Spec.Networks, newNetworkConfigs(network1, network2)...)
	task := newTask("task", service)

	createDBObjects(t, s, service, network1, network2, task)

	deallocator, ran := startNewDeallocator(t, s,
		// nothing should have been deleted
		false)
	defer stopDeallocator(t, deallocator, ran)

	// now let's delete the one task saving it all from oblivion
	updateStoreAndWaitForEvent(t, deallocator, func(tx store.Tx) {
		require.NoError(t, store.DeleteTask(tx, task.ID))
	}, true)

	// the deallocator should have removed both the service and
	// the first network, but not the second
	s.View(func(tx store.ReadTx) {
		require.Nil(t, store.GetService(tx, service.ID))
		require.Nil(t, store.GetNetwork(tx, network1.ID))
		require.NotNil(t, store.GetNetwork(tx, network2.ID))
	})
}

// Helpers below

// starts a new deallocator, and also creates a channel to retrieve the return
// value, so that we can check later than there was no error
func startNewDeallocator(t *testing.T, s *store.MemoryStore, expectedUpdates bool) (deallocator *Deallocator, ran chan error) {
	deallocator = New(s)
	deallocator.eventChan = make(chan bool)
	ran = make(chan error)

	go func() {
		returnValue := deallocator.Run(context.Background())
		// allows checking that `Run` does return after we've stopped
		ran <- returnValue
		close(ran)
	}()
	waitForDeallocatorEvent(t, deallocator, expectedUpdates)

	return
}

// stops the deallocator started by `startNewDeallocator` above
func stopDeallocator(t *testing.T, deallocator *Deallocator, ran chan error) {
	stopped := make(chan struct{})
	go func() {
		deallocator.Stop()
		close(stopped)
	}()

	// it shouldn't take too long to stop
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("Waited for too long for the deallocator to stop")
	}

	// `Run` should have returned, too
	select {
	case returnValue := <-ran:
		require.NoError(t, returnValue)
	case <-time.After(time.Second):
		t.Fatal("Run hasn't returned")
	}

	ensureNoDeallocatorEvent(t, deallocator)
}

func waitForDeallocatorEvent(t *testing.T, deallocator *Deallocator, expectedUpdates bool) {
	select {
	case updates := <-deallocator.eventChan:
		assert.Equalf(t, expectedUpdates, updates, "Expected updates %v VS actual %v", expectedUpdates, updates)
		ensureNoDeallocatorEvent(t, deallocator)
	case <-time.After(time.Second):
		t.Fatal("Waited for too long for the deallocator to process new events")
	}
}

func ensureNoDeallocatorEvent(t *testing.T, deallocator *Deallocator) {
	select {
	case <-deallocator.eventChan:
		t.Fatal("Found unexpected event")
	default:
	}
}

func createDBObjects(t *testing.T, s *store.MemoryStore, objects ...interface{}) {
	err := s.Update(func(tx store.Tx) (e error) {
		for _, object := range objects {
			switch typedObject := object.(type) {
			case *api.Service:
				e = store.CreateService(tx, typedObject)
			case *api.Task:
				e = store.CreateTask(tx, typedObject)
			case *api.Network:
				e = store.CreateNetwork(tx, typedObject)
			}
			require.NoError(t, e, "Unable to create DB object %v", object)
		}
		return
	})
	require.NoError(t, err, "Error setting up test fixtures")
}

func updateStore(t *testing.T, s *store.MemoryStore, cb func(x store.Tx)) {
	require.NoError(t, s.Update(func(tx store.Tx) error {
		cb(tx)
		return nil
	}))
}

func updateStoreAndWaitForEvent(t *testing.T, deallocator *Deallocator, cb func(x store.Tx), expectedUpdates bool) {
	updateStore(t, deallocator.store, cb)
	waitForDeallocatorEvent(t, deallocator, expectedUpdates)
}

func newService(id string, pendingDelete bool, networks ...*api.Network) *api.Service {
	return &api.Service{
		ID: id,
		Spec: api.ServiceSpec{
			Annotations: api.Annotations{
				Name: id,
			},
			Task: api.TaskSpec{
				Networks: newNetworkConfigs(networks...),
			},
		},
		PendingDelete: pendingDelete,
	}
}

func newNetwork(id string, pendingDelete bool) *api.Network {
	return &api.Network{
		ID: id,
		Spec: api.NetworkSpec{
			Annotations: api.Annotations{
				Name: id,
			},
		},
		PendingDelete: pendingDelete,
	}
}

func newNetworkConfigs(networks ...*api.Network) []*api.NetworkAttachmentConfig {
	networkConfigs := make([]*api.NetworkAttachmentConfig, len(networks))

	for i := 0; i < len(networks); i++ {
		networkConfigs[i] = &api.NetworkAttachmentConfig{
			Target: networks[i].ID,
		}
	}

	return networkConfigs
}

func newTask(id string, service *api.Service) *api.Task {
	return &api.Task{
		ID:        id,
		ServiceID: service.ID,
	}
}
