package allocator

import (
	"fmt"
	"net"
	"testing"
	"time"

	"golang.org/x/net/context"

	"github.com/docker/go-events"
	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/manager/state"
	"github.com/docker/swarm-v2/manager/state/store"
	"github.com/stretchr/testify/assert"
)

func TestAllocator(t *testing.T) {
	store := store.NewMemoryStore(nil)
	assert.NotNil(t, store)

	a, err := New(store)
	assert.NoError(t, err)
	assert.NotNil(t, a)

	// Try adding some objects to store before allocator is started
	assert.NoError(t, store.Update(func(tx state.Tx) error {
		n1 := &api.Network{
			ID: "testID1",
			Spec: api.NetworkSpec{
				Annotations: api.Annotations{
					Name: "test1",
				},
			},
		}
		assert.NoError(t, tx.Networks().Create(n1))
		return nil
	}))

	assert.NoError(t, store.Update(func(tx state.Tx) error {
		s1 := &api.Service{
			ID: "testServiceID1",
			Spec: api.ServiceSpec{
				Annotations: api.Annotations{
					Name: "service1",
				},
				Endpoint: &api.Endpoint{
					Ports: []*api.Endpoint_PortConfiguration{
						{
							Name: "http",
							Port: 80,
						},
					},
				},
			},
		}
		assert.NoError(t, tx.Services().Create(s1))
		return nil
	}))

	assert.NoError(t, store.Update(func(tx state.Tx) error {
		t1 := &api.Task{
			ID: "testTaskID1",
			Status: api.TaskStatus{
				State: api.TaskStateNew,
			},
			Spec: api.TaskSpec{
				Runtime: &api.TaskSpec_Container{
					Container: &api.Container{
						Networks: []*api.Container_NetworkAttachment{
							{
								Reference: &api.Container_NetworkAttachment_NetworkID{
									NetworkID: "testID1",
								},
							},
						},
					},
				},
			},
		}
		assert.NoError(t, tx.Tasks().Create(t1))
		return nil
	}))

	netWatch, cancel := state.Watch(store.WatchQueue(), state.EventUpdateNetwork{}, state.EventDeleteNetwork{})
	defer cancel()
	taskWatch, cancel := state.Watch(store.WatchQueue(), state.EventUpdateTask{}, state.EventDeleteTask{})
	defer cancel()
	serviceWatch, cancel := state.Watch(store.WatchQueue(), state.EventUpdateService{}, state.EventDeleteService{})
	defer cancel()

	// Start allocator
	assert.NoError(t, a.Start(context.Background()))

	// Now verify if we get network and tasks updated properly
	n1, err := watchNetwork(t, netWatch)
	assert.NoError(t, err)
	assert.NotEqual(t, n1.IPAM.Configurations, nil)
	assert.Equal(t, len(n1.IPAM.Configurations), 1)
	assert.Equal(t, n1.IPAM.Configurations[0].Range, "")
	assert.Equal(t, len(n1.IPAM.Configurations[0].Reserved), 0)

	_, subnet, err := net.ParseCIDR(n1.IPAM.Configurations[0].Subnet)
	assert.NoError(t, err)

	ip := net.ParseIP(n1.IPAM.Configurations[0].Gateway)
	assert.NotEqual(t, ip, nil)

	s1, err := watchService(t, serviceWatch)
	assert.NoError(t, err)
	assert.Equal(t, 1, len(s1.Endpoint.Ports))
	assert.NotEqual(t, 0, s1.Endpoint.Ports[0].NodePort)

	t1, err := watchTask(t, taskWatch)
	assert.NoError(t, err)
	assert.Equal(t, len(t1.Networks[0].Addresses), 1)
	ip, _, err = net.ParseCIDR(t1.Networks[0].Addresses[0])
	assert.NoError(t, err)
	assert.Equal(t, subnet.Contains(ip), true)
	assert.Equal(t, t1.Status.State, api.TaskStateAllocated)

	// Add new networks/tasks/services after allocator is started.
	assert.NoError(t, store.Update(func(tx state.Tx) error {
		n2 := &api.Network{
			ID: "testID2",
			Spec: api.NetworkSpec{
				Annotations: api.Annotations{
					Name: "test2",
				},
			},
		}
		assert.NoError(t, tx.Networks().Create(n2))
		return nil
	}))

	n2, err := watchNetwork(t, netWatch)
	assert.NoError(t, err)
	assert.NotEqual(t, n2.IPAM.Configurations, nil)
	assert.Equal(t, len(n2.IPAM.Configurations), 1)
	assert.Equal(t, n2.IPAM.Configurations[0].Range, "")
	assert.Equal(t, len(n2.IPAM.Configurations[0].Reserved), 0)

	_, subnet, err = net.ParseCIDR(n2.IPAM.Configurations[0].Subnet)
	assert.NoError(t, err)

	ip = net.ParseIP(n2.IPAM.Configurations[0].Gateway)
	assert.NotEqual(t, ip, nil)

	assert.NoError(t, store.Update(func(tx state.Tx) error {
		s2 := &api.Service{
			ID: "testServiceID2",
			Spec: api.ServiceSpec{
				Annotations: api.Annotations{
					Name: "service2",
				},
				Endpoint: &api.Endpoint{
					Ports: []*api.Endpoint_PortConfiguration{
						{
							Name: "http",
							Port: 80,
						},
					},
				},
			},
		}
		assert.NoError(t, tx.Services().Create(s2))
		return nil
	}))

	s2, err := watchService(t, serviceWatch)
	assert.NoError(t, err)
	assert.Equal(t, 1, len(s2.Endpoint.Ports))
	assert.NotEqual(t, 0, s2.Endpoint.Ports[0].NodePort)

	assert.NoError(t, store.Update(func(tx state.Tx) error {
		t2 := &api.Task{
			ID: "testTaskID2",
			Status: api.TaskStatus{
				State: api.TaskStateNew,
			},
			ServiceID:    "testServiceID2",
			DesiredState: api.TaskStateRunning,
			Spec: api.TaskSpec{
				Runtime: &api.TaskSpec_Container{
					Container: &api.Container{
						Networks: []*api.Container_NetworkAttachment{
							{
								Reference: &api.Container_NetworkAttachment_NetworkID{
									NetworkID: "testID2",
								},
							},
						},
					},
				},
			},
		}
		assert.NoError(t, tx.Tasks().Create(t2))
		return nil
	}))

	t2, err := watchTask(t, taskWatch)
	assert.NoError(t, err)
	assert.Equal(t, len(t2.Networks[0].Addresses), 1)
	ip, _, err = net.ParseCIDR(t2.Networks[0].Addresses[0])
	assert.NoError(t, err)
	assert.Equal(t, subnet.Contains(ip), true)
	assert.Equal(t, t2.Status.State, api.TaskStateAllocated)
	assert.NotEqual(t, nil, t2.Endpoint)
	assert.Equal(t, s2.Endpoint, t2.Endpoint)

	// Now try adding a task which depends on a network before adding the network.
	assert.NoError(t, store.Update(func(tx state.Tx) error {
		t3 := &api.Task{
			ID: "testTaskID3",
			Status: api.TaskStatus{
				State: api.TaskStateNew,
			},
			DesiredState: api.TaskStateRunning,
			Spec: api.TaskSpec{
				Runtime: &api.TaskSpec_Container{
					Container: &api.Container{
						Networks: []*api.Container_NetworkAttachment{
							{
								Reference: &api.Container_NetworkAttachment_NetworkID{
									NetworkID: "testID3",
								},
							},
						},
					},
				},
			},
		}
		assert.NoError(t, tx.Tasks().Create(t3))
		return nil
	}))

	// Wait for a little bit of time before adding network just to
	// test network is not available while task allocation is
	// going through
	time.Sleep(10 * time.Millisecond)

	assert.NoError(t, store.Update(func(tx state.Tx) error {
		n3 := &api.Network{
			ID: "testID3",
			Spec: api.NetworkSpec{
				Annotations: api.Annotations{
					Name: "test3",
				},
			},
		}
		assert.NoError(t, tx.Networks().Create(n3))
		return nil
	}))

	n3, err := watchNetwork(t, netWatch)
	assert.NoError(t, err)
	assert.NotEqual(t, n3.IPAM.Configurations, nil)
	assert.Equal(t, len(n3.IPAM.Configurations), 1)
	assert.Equal(t, n3.IPAM.Configurations[0].Range, "")
	assert.Equal(t, len(n3.IPAM.Configurations[0].Reserved), 0)

	_, subnet, err = net.ParseCIDR(n3.IPAM.Configurations[0].Subnet)
	assert.NoError(t, err)

	ip = net.ParseIP(n3.IPAM.Configurations[0].Gateway)
	assert.NotEqual(t, ip, nil)

	t3, err := watchTask(t, taskWatch)
	assert.NoError(t, err)
	assert.Equal(t, len(t3.Networks[0].Addresses), 1)
	ip, _, err = net.ParseCIDR(t3.Networks[0].Addresses[0])
	assert.NoError(t, err)
	assert.Equal(t, subnet.Contains(ip), true)
	assert.Equal(t, t3.Status.State, api.TaskStateAllocated)

	assert.NoError(t, store.Update(func(tx state.Tx) error {
		assert.NoError(t, tx.Tasks().Delete("testTaskID3"))
		return nil
	}))
	_, err = watchTask(t, taskWatch)
	assert.NoError(t, err)

	assert.NoError(t, store.Update(func(tx state.Tx) error {
		t5 := &api.Task{
			ID: "testTaskID5",
			Status: api.TaskStatus{
				State: api.TaskStateNew,
			},
			DesiredState: api.TaskStateRunning,
			ServiceID:    "testServiceID2",
			Spec: api.TaskSpec{
				Runtime: &api.TaskSpec_Container{
					Container: &api.Container{},
				},
			},
		}
		assert.NoError(t, tx.Tasks().Create(t5))
		return nil
	}))
	t5, err := watchTask(t, taskWatch)
	assert.NoError(t, err)
	assert.Equal(t, t5.Status.State, api.TaskStateAllocated)

	assert.NoError(t, store.Update(func(tx state.Tx) error {
		assert.NoError(t, tx.Networks().Delete("testID3"))
		return nil
	}))
	_, err = watchNetwork(t, netWatch)
	assert.NoError(t, err)

	assert.NoError(t, store.Update(func(tx state.Tx) error {
		assert.NoError(t, tx.Services().Delete("testServiceID2"))
		return nil
	}))
	_, err = watchService(t, serviceWatch)
	assert.NoError(t, err)

	// Try to create a task with no network attachments and test
	// that it moves to ALLOCATED state.
	assert.NoError(t, store.Update(func(tx state.Tx) error {
		t4 := &api.Task{
			ID: "testTaskID4",
			Status: api.TaskStatus{
				State: api.TaskStateNew,
			},
			DesiredState: api.TaskStateRunning,
			Spec: api.TaskSpec{
				Runtime: &api.TaskSpec_Container{
					Container: &api.Container{},
				},
			},
		}
		assert.NoError(t, tx.Tasks().Create(t4))
		return nil
	}))
	t4, err := watchTask(t, taskWatch)
	assert.NoError(t, err)
	assert.Equal(t, t4.Status.State, api.TaskStateAllocated)

	assert.NoError(t, store.Update(func(tx state.Tx) error {
		assert.NoError(t, tx.Networks().Update(n2))
		return nil
	}))
	n2, err = watchNetwork(t, netWatch)
	assert.NoError(t, err)
	n2, err = watchNetwork(t, netWatch)
	assert.Error(t, err)

	// Try updating task which is already allocated
	assert.NoError(t, store.Update(func(tx state.Tx) error {
		assert.NoError(t, tx.Tasks().Update(t2))
		return nil
	}))
	_, err = watchTask(t, taskWatch)
	assert.NoError(t, err)
	_, err = watchTask(t, taskWatch)
	assert.Error(t, err)

	a.Stop()
}

func watchNetwork(t *testing.T, watch chan events.Event) (*api.Network, error) {
	for {
		select {
		case event := <-watch:
			if n, ok := event.(state.EventUpdateNetwork); ok {
				return n.Network, nil
			}
			if n, ok := event.(state.EventDeleteNetwork); ok {
				return n.Network, nil
			}

			return nil, fmt.Errorf("got event %T when expecting EventUpdateNetwork/EventDeleteNetwork", event)
		case <-time.After(250 * time.Millisecond):
			return nil, fmt.Errorf("timed out")

		}
	}
}

func watchService(t *testing.T, watch chan events.Event) (*api.Service, error) {
	for {
		select {
		case event := <-watch:
			if s, ok := event.(state.EventUpdateService); ok {
				return s.Service, nil
			}
			if s, ok := event.(state.EventDeleteService); ok {
				return s.Service, nil
			}

			return nil, fmt.Errorf("got event %T when expecting EventUpdateService/EventDeleteService", event)
		case <-time.After(250 * time.Millisecond):
			return nil, fmt.Errorf("timed out")

		}
	}
}

func watchTask(t *testing.T, watch chan events.Event) (*api.Task, error) {
	for {
		select {
		case event := <-watch:
			if t, ok := event.(state.EventUpdateTask); ok {
				return t.Task, nil
			}
			if t, ok := event.(state.EventDeleteTask); ok {
				return t.Task, nil
			}
			return nil, fmt.Errorf("got event %T when expecting EventUpdateTask/EventDeleteTask", event)
		case <-time.After(250 * time.Millisecond):
			return nil, fmt.Errorf("timed out")

		}
	}
}
