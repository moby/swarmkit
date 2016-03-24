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
	"github.com/stretchr/testify/assert"
)

func TestAllocator(t *testing.T) {
	store := state.NewMemoryStore(nil)
	assert.NotNil(t, store)

	a, err := New(store)
	assert.NoError(t, err)
	assert.NotNil(t, a)

	// Try adding some objects to store before allocator is started
	assert.NoError(t, store.Update(func(tx state.Tx) error {
		n1 := &api.Network{
			ID: "testID1",
			Spec: &api.NetworkSpec{
				Meta: api.Meta{
					Name: "test1",
				},
			},
		}
		assert.NoError(t, tx.Networks().Create(n1))
		return nil
	}))

	assert.NoError(t, store.Update(func(tx state.Tx) error {
		t1 := &api.Task{
			ID: "testTaskID1",
			Status: &api.TaskStatus{
				State: api.TaskStateNew,
			},
			Spec: &api.TaskSpec{
				Runtime: &api.TaskSpec_Container{
					Container: &api.ContainerSpec{
						Networks: []*api.ContainerSpec_NetworkAttachmentSpec{
							{
								Reference: &api.ContainerSpec_NetworkAttachmentSpec_NetworkID{
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

	// Start allocator
	assert.NoError(t, a.Start(context.Background()))

	// Now verify if we get network and tasks updated properly
	n1, err := watchNetwork(t, netWatch)
	assert.NoError(t, err)
	assert.NotEqual(t, n1.Spec.IPAM.Configurations, nil)
	assert.Equal(t, len(n1.Spec.IPAM.Configurations), 1)
	assert.Equal(t, n1.Spec.IPAM.Configurations[0].Range, "")
	assert.Equal(t, len(n1.Spec.IPAM.Configurations[0].Reserved), 0)

	_, subnet, err := net.ParseCIDR(n1.Spec.IPAM.Configurations[0].Subnet)
	assert.NoError(t, err)

	ip := net.ParseIP(n1.Spec.IPAM.Configurations[0].Gateway)
	assert.NotEqual(t, ip, nil)

	t1, err := watchTask(t, taskWatch)
	assert.NoError(t, err)
	assert.Equal(t, len(t1.Networks[0].Addresses), 1)
	ip, _, err = net.ParseCIDR(t1.Networks[0].Addresses[0])
	assert.NoError(t, err)
	assert.Equal(t, subnet.Contains(ip), true)
	assert.Equal(t, t1.Status.State, api.TaskStateAllocated)

	// Add new networks and tasks after allocator is started.
	assert.NoError(t, store.Update(func(tx state.Tx) error {
		n2 := &api.Network{
			ID: "testID2",
			Spec: &api.NetworkSpec{
				Meta: api.Meta{
					Name: "test2",
				},
			},
		}
		assert.NoError(t, tx.Networks().Create(n2))
		return nil
	}))

	n2, err := watchNetwork(t, netWatch)
	assert.NoError(t, err)
	assert.NotEqual(t, n2.Spec.IPAM.Configurations, nil)
	assert.Equal(t, len(n2.Spec.IPAM.Configurations), 1)
	assert.Equal(t, n2.Spec.IPAM.Configurations[0].Range, "")
	assert.Equal(t, len(n2.Spec.IPAM.Configurations[0].Reserved), 0)

	_, subnet, err = net.ParseCIDR(n2.Spec.IPAM.Configurations[0].Subnet)
	assert.NoError(t, err)

	ip = net.ParseIP(n2.Spec.IPAM.Configurations[0].Gateway)
	assert.NotEqual(t, ip, nil)

	assert.NoError(t, store.Update(func(tx state.Tx) error {
		t2 := &api.Task{
			ID: "testTaskID2",
			Status: &api.TaskStatus{
				State: api.TaskStateNew,
			},
			Spec: &api.TaskSpec{
				Runtime: &api.TaskSpec_Container{
					Container: &api.ContainerSpec{
						Networks: []*api.ContainerSpec_NetworkAttachmentSpec{
							{
								Reference: &api.ContainerSpec_NetworkAttachmentSpec_NetworkID{
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

	// Now try adding a task which depends on a network before adding the network.
	assert.NoError(t, store.Update(func(tx state.Tx) error {
		t3 := &api.Task{
			ID: "testTaskID3",
			Status: &api.TaskStatus{
				State: api.TaskStateNew,
			},
			Spec: &api.TaskSpec{
				Runtime: &api.TaskSpec_Container{
					Container: &api.ContainerSpec{
						Networks: []*api.ContainerSpec_NetworkAttachmentSpec{
							{
								Reference: &api.ContainerSpec_NetworkAttachmentSpec_NetworkID{
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
			Spec: &api.NetworkSpec{
				Meta: api.Meta{
					Name: "test3",
				},
			},
		}
		assert.NoError(t, tx.Networks().Create(n3))
		return nil
	}))

	n3, err := watchNetwork(t, netWatch)
	assert.NoError(t, err)
	assert.NotEqual(t, n3.Spec.IPAM.Configurations, nil)
	assert.Equal(t, len(n3.Spec.IPAM.Configurations), 1)
	assert.Equal(t, n3.Spec.IPAM.Configurations[0].Range, "")
	assert.Equal(t, len(n3.Spec.IPAM.Configurations[0].Reserved), 0)

	_, subnet, err = net.ParseCIDR(n3.Spec.IPAM.Configurations[0].Subnet)
	assert.NoError(t, err)

	ip = net.ParseIP(n3.Spec.IPAM.Configurations[0].Gateway)
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
		assert.NoError(t, tx.Networks().Delete("testID3"))
		return nil
	}))
	_, err = watchNetwork(t, netWatch)
	assert.NoError(t, err)

	assert.NoError(t, store.Update(func(tx state.Tx) error {
		t4 := &api.Task{
			ID: "testTaskID4",
			Status: &api.TaskStatus{
				State: api.TaskStateNew,
			},
			Spec: &api.TaskSpec{},
		}
		assert.NoError(t, tx.Tasks().Create(t4))
		return nil
	}))

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
