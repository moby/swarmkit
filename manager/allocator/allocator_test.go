package allocator

import (
	"net"
	"runtime/debug"
	"testing"
	"time"

	"golang.org/x/net/context"

	"github.com/docker/go-events"
	"github.com/docker/swarmkit/api"
	"github.com/docker/swarmkit/manager/state"
	"github.com/docker/swarmkit/manager/state/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAllocator(t *testing.T) {
	s := store.NewMemoryStore(nil)
	assert.NotNil(t, s)
	defer s.Close()

	a, err := New(s, nil)
	assert.NoError(t, err)
	assert.NotNil(t, a)

	// Try adding some objects to store before allocator is started
	assert.NoError(t, s.Update(func(tx store.Tx) error {
		n1 := &api.Network{
			ID: "testID1",
			Spec: api.NetworkSpec{
				Annotations: api.Annotations{
					Name: "test1",
				},
			},
		}
		assert.NoError(t, store.CreateNetwork(tx, n1))

		s1 := &api.Service{
			ID: "testServiceID1",
			Spec: api.ServiceSpec{
				Annotations: api.Annotations{
					Name: "service1",
				},
				Task: api.TaskSpec{
					Networks: []*api.NetworkAttachmentConfig{
						{
							Target: "testID1",
						},
					},
				},
				Endpoint: &api.EndpointSpec{
					Mode: api.ResolutionModeVirtualIP,
					Ports: []*api.PortConfig{
						{
							Name:          "portName",
							Protocol:      api.ProtocolTCP,
							TargetPort:    8000,
							PublishedPort: 8001,
						},
					},
				},
			},
		}
		assert.NoError(t, store.CreateService(tx, s1))

		t1 := &api.Task{
			ID: "testTaskID1",
			Status: api.TaskStatus{
				State: api.TaskStateNew,
			},
			Networks: []*api.NetworkAttachment{
				{
					Network: n1,
				},
			},
		}
		assert.NoError(t, store.CreateTask(tx, t1))

		t2 := &api.Task{
			ID: "testTaskIDPreInit",
			Status: api.TaskStatus{
				State: api.TaskStateNew,
			},
			ServiceID:    "testServiceID1",
			DesiredState: api.TaskStateRunning,
		}
		assert.NoError(t, store.CreateTask(tx, t2))
		return nil
	}))

	netWatch, cancel := state.Watch(s.WatchQueue(), state.EventUpdateNetwork{}, state.EventDeleteNetwork{})
	defer cancel()
	taskWatch, cancel := state.Watch(s.WatchQueue(), state.EventUpdateTask{}, state.EventDeleteTask{})
	defer cancel()
	serviceWatch, cancel := state.Watch(s.WatchQueue(), state.EventUpdateService{}, state.EventDeleteService{})
	defer cancel()

	// Start allocator
	go func() {
		assert.NoError(t, a.Run(context.Background()))
	}()

	// Now verify if we get network and tasks updated properly
	watchNetwork(t, netWatch, false, isValidNetwork)
	watchTask(t, s, taskWatch, false, isValidTask) // t1
	watchTask(t, s, taskWatch, false, isValidTask) // t2
	watchService(t, serviceWatch, false, nil)

	// Add new networks/tasks/services after allocator is started.
	assert.NoError(t, s.Update(func(tx store.Tx) error {
		n2 := &api.Network{
			ID: "testID2",
			Spec: api.NetworkSpec{
				Annotations: api.Annotations{
					Name: "test2",
				},
			},
		}
		assert.NoError(t, store.CreateNetwork(tx, n2))
		return nil
	}))

	watchNetwork(t, netWatch, false, isValidNetwork)

	assert.NoError(t, s.Update(func(tx store.Tx) error {
		s2 := &api.Service{
			ID: "testServiceID2",
			Spec: api.ServiceSpec{
				Annotations: api.Annotations{
					Name: "service2",
				},
				Networks: []*api.NetworkAttachmentConfig{
					{
						Target: "testID2",
					},
				},
				Endpoint: &api.EndpointSpec{},
			},
		}
		assert.NoError(t, store.CreateService(tx, s2))
		return nil
	}))

	watchService(t, serviceWatch, false, nil)

	assert.NoError(t, s.Update(func(tx store.Tx) error {
		t2 := &api.Task{
			ID: "testTaskID2",
			Status: api.TaskStatus{
				State: api.TaskStateNew,
			},
			ServiceID:    "testServiceID2",
			DesiredState: api.TaskStateRunning,
		}
		assert.NoError(t, store.CreateTask(tx, t2))
		return nil
	}))

	watchTask(t, s, taskWatch, false, isValidTask)

	// Now try adding a task which depends on a network before adding the network.
	n3 := &api.Network{
		ID: "testID3",
		Spec: api.NetworkSpec{
			Annotations: api.Annotations{
				Name: "test3",
			},
		},
	}

	assert.NoError(t, s.Update(func(tx store.Tx) error {
		t3 := &api.Task{
			ID: "testTaskID3",
			Status: api.TaskStatus{
				State: api.TaskStateNew,
			},
			DesiredState: api.TaskStateRunning,
			Networks: []*api.NetworkAttachment{
				{
					Network: n3,
				},
			},
		}
		assert.NoError(t, store.CreateTask(tx, t3))
		return nil
	}))

	// Wait for a little bit of time before adding network just to
	// test network is not available while task allocation is
	// going through
	time.Sleep(10 * time.Millisecond)

	assert.NoError(t, s.Update(func(tx store.Tx) error {
		assert.NoError(t, store.CreateNetwork(tx, n3))
		return nil
	}))

	watchNetwork(t, netWatch, false, isValidNetwork)
	watchTask(t, s, taskWatch, false, isValidTask)

	assert.NoError(t, s.Update(func(tx store.Tx) error {
		assert.NoError(t, store.DeleteTask(tx, "testTaskID3"))
		return nil
	}))
	watchTask(t, s, taskWatch, false, isValidTask)

	assert.NoError(t, s.Update(func(tx store.Tx) error {
		t5 := &api.Task{
			ID: "testTaskID5",
			Spec: api.TaskSpec{
				Networks: []*api.NetworkAttachmentConfig{
					{
						Target: "testID2",
					},
				},
			},
			Status: api.TaskStatus{
				State: api.TaskStateNew,
			},
			DesiredState: api.TaskStateRunning,
			ServiceID:    "testServiceID2",
		}
		assert.NoError(t, store.CreateTask(tx, t5))
		return nil
	}))
	watchTask(t, s, taskWatch, false, isValidTask)

	assert.NoError(t, s.Update(func(tx store.Tx) error {
		assert.NoError(t, store.DeleteNetwork(tx, "testID3"))
		return nil
	}))
	watchNetwork(t, netWatch, false, isValidNetwork)

	assert.NoError(t, s.Update(func(tx store.Tx) error {
		assert.NoError(t, store.DeleteService(tx, "testServiceID2"))
		return nil
	}))
	watchService(t, serviceWatch, false, nil)

	// Try to create a task with no network attachments and test
	// that it moves to ALLOCATED state.
	assert.NoError(t, s.Update(func(tx store.Tx) error {
		t4 := &api.Task{
			ID: "testTaskID4",
			Status: api.TaskStatus{
				State: api.TaskStateNew,
			},
			DesiredState: api.TaskStateRunning,
		}
		assert.NoError(t, store.CreateTask(tx, t4))
		return nil
	}))
	watchTask(t, s, taskWatch, false, isValidTask)

	assert.NoError(t, s.Update(func(tx store.Tx) error {
		n2 := store.GetNetwork(tx, "testID2")
		require.NotEqual(t, nil, n2)
		assert.NoError(t, store.UpdateNetwork(tx, n2))
		return nil
	}))
	watchNetwork(t, netWatch, false, isValidNetwork)
	watchNetwork(t, netWatch, true, nil)

	// Try updating service which is already allocated with no endpointSpec
	assert.NoError(t, s.Update(func(tx store.Tx) error {
		s := store.GetService(tx, "testServiceID1")
		s.Spec.Endpoint = nil

		assert.NoError(t, store.UpdateService(tx, s))
		return nil
	}))
	watchService(t, serviceWatch, false, nil)

	// Try updating task which is already allocated
	assert.NoError(t, s.Update(func(tx store.Tx) error {
		t2 := store.GetTask(tx, "testTaskID2")
		require.NotEqual(t, nil, t2)
		assert.NoError(t, store.UpdateTask(tx, t2))
		return nil
	}))
	watchTask(t, s, taskWatch, false, isValidTask)
	watchTask(t, s, taskWatch, true, nil)

	// Try adding networks with conflicting network resources and
	// add task which attaches to a network which gets allocated
	// later and verify if task reconciles and moves to ALLOCATED.
	n4 := &api.Network{
		ID: "testID4",
		Spec: api.NetworkSpec{
			Annotations: api.Annotations{
				Name: "test4",
			},
			DriverConfig: &api.Driver{
				Name: "overlay",
				Options: map[string]string{
					"com.docker.network.driver.overlay.vxlanid_list": "328",
				},
			},
		},
	}

	n5 := n4.Copy()
	n5.ID = "testID5"
	n5.Spec.Annotations.Name = "test5"
	assert.NoError(t, s.Update(func(tx store.Tx) error {
		assert.NoError(t, store.CreateNetwork(tx, n4))
		return nil
	}))
	watchNetwork(t, netWatch, false, isValidNetwork)

	assert.NoError(t, s.Update(func(tx store.Tx) error {
		assert.NoError(t, store.CreateNetwork(tx, n5))
		return nil
	}))
	watchNetwork(t, netWatch, true, nil)

	assert.NoError(t, s.Update(func(tx store.Tx) error {
		t6 := &api.Task{
			ID: "testTaskID6",
			Status: api.TaskStatus{
				State: api.TaskStateNew,
			},
			DesiredState: api.TaskStateRunning,
			Networks: []*api.NetworkAttachment{
				{
					Network: n5,
				},
			},
		}
		assert.NoError(t, store.CreateTask(tx, t6))
		return nil
	}))
	watchTask(t, s, taskWatch, true, nil)

	// Now remove the conflicting network.
	assert.NoError(t, s.Update(func(tx store.Tx) error {
		assert.NoError(t, store.DeleteNetwork(tx, n4.ID))
		return nil
	}))
	watchNetwork(t, netWatch, false, isValidNetwork)
	watchTask(t, s, taskWatch, false, isValidTask)

	// Try adding services with conflicting port configs and add
	// task which is part of the service whose allocation hasn't
	// happened and when that happens later and verify if task
	// reconciles and moves to ALLOCATED.
	s3 := &api.Service{
		ID: "testServiceID3",
		Spec: api.ServiceSpec{
			Annotations: api.Annotations{
				Name: "service3",
			},
			Endpoint: &api.EndpointSpec{
				Ports: []*api.PortConfig{
					{
						Name:          "http",
						TargetPort:    80,
						PublishedPort: 8080,
					},
					{
						PublishMode: api.PublishModeHost,
						Name:        "http",
						TargetPort:  80,
					},
				},
			},
		},
	}

	s4 := s3.Copy()
	s4.ID = "testServiceID4"
	s4.Spec.Annotations.Name = "service4"
	assert.NoError(t, s.Update(func(tx store.Tx) error {
		assert.NoError(t, store.CreateService(tx, s3))
		return nil
	}))
	watchService(t, serviceWatch, false, nil)
	assert.NoError(t, s.Update(func(tx store.Tx) error {
		assert.NoError(t, store.CreateService(tx, s4))
		return nil
	}))
	watchService(t, serviceWatch, true, nil)

	assert.NoError(t, s.Update(func(tx store.Tx) error {
		t7 := &api.Task{
			ID: "testTaskID7",
			Status: api.TaskStatus{
				State: api.TaskStateNew,
			},
			ServiceID:    "testServiceID4",
			DesiredState: api.TaskStateRunning,
		}
		assert.NoError(t, store.CreateTask(tx, t7))
		return nil
	}))
	watchTask(t, s, taskWatch, true, nil)

	// Now remove the conflicting service.
	assert.NoError(t, s.Update(func(tx store.Tx) error {
		assert.NoError(t, store.DeleteService(tx, s3.ID))
		return nil
	}))
	watchService(t, serviceWatch, false, nil)
	watchTask(t, s, taskWatch, false, isValidTask)

	a.Stop()
}

func isValidNetwork(t assert.TestingT, n *api.Network) bool {
	return assert.NotEqual(t, n.IPAM.Configs, nil) &&
		assert.Equal(t, len(n.IPAM.Configs), 1) &&
		assert.Equal(t, n.IPAM.Configs[0].Range, "") &&
		assert.Equal(t, len(n.IPAM.Configs[0].Reserved), 0) &&
		isValidSubnet(t, n.IPAM.Configs[0].Subnet) &&
		assert.NotEqual(t, net.ParseIP(n.IPAM.Configs[0].Gateway), nil)
}

func isValidTask(t assert.TestingT, s *store.MemoryStore, task *api.Task) bool {
	return isValidNetworkAttachment(t, task) &&
		isValidEndpoint(t, s, task) &&
		assert.Equal(t, task.Status.State, api.TaskStatePending)
}

func isValidNetworkAttachment(t assert.TestingT, task *api.Task) bool {
	if len(task.Networks) != 0 {
		return assert.Equal(t, len(task.Networks[0].Addresses), 1) &&
			isValidSubnet(t, task.Networks[0].Addresses[0])
	}

	return true
}

func isValidEndpoint(t assert.TestingT, s *store.MemoryStore, task *api.Task) bool {
	if task.ServiceID != "" {
		var service *api.Service
		s.View(func(tx store.ReadTx) {
			service = store.GetService(tx, task.ServiceID)
		})

		if service == nil {
			return true
		}

		return assert.Equal(t, service.Endpoint, task.Endpoint)

	}

	return true
}

func isValidSubnet(t assert.TestingT, subnet string) bool {
	_, _, err := net.ParseCIDR(subnet)
	return assert.NoError(t, err)
}

type mockTester struct{}

func (m mockTester) Errorf(format string, args ...interface{}) {
}

// Returns a timeout given whether we should expect a timeout:  In the case where we do expect a timeout,
// the timeout should be short, because it's not very useful to wait long amounts of time just in case
// an unexpected event comes in - a short timeout should catch an incorrect event at least often enough
// to make the test flaky and alert us to the problem. But in the cases where we don't expect a timeout,
// the timeout should be on the order of serveral seconds, so the test doesn't fail just because it's run
// on a relatively slow system, or there's a load spike.
func getWatchTimeout(expectTimeout bool) time.Duration {
	if expectTimeout {
		return 350 * time.Millisecond
	}
	return 5 * time.Second
}

func watchNetwork(t *testing.T, watch chan events.Event, expectTimeout bool, fn func(t assert.TestingT, n *api.Network) bool) {
	for {
		var network *api.Network
		select {
		case event := <-watch:
			if n, ok := event.(state.EventUpdateNetwork); ok {
				network = n.Network.Copy()
				if fn == nil || (fn != nil && fn(mockTester{}, network)) {
					return
				}
			}

			if n, ok := event.(state.EventDeleteNetwork); ok {
				network = n.Network.Copy()
				if fn == nil || (fn != nil && fn(mockTester{}, network)) {
					return
				}
			}

		case <-time.After(getWatchTimeout(expectTimeout)):
			if !expectTimeout {
				if network != nil && fn != nil {
					fn(t, network)
				}

				t.Fatal("timed out before watchNetwork found expected network state")
			}

			return
		}
	}
}

func watchService(t *testing.T, watch chan events.Event, expectTimeout bool, fn func(t assert.TestingT, n *api.Service) bool) {
	for {
		var service *api.Service
		select {
		case event := <-watch:
			if s, ok := event.(state.EventUpdateService); ok {
				service = s.Service.Copy()
				if fn == nil || (fn != nil && fn(mockTester{}, service)) {
					return
				}
			}

			if s, ok := event.(state.EventDeleteService); ok {
				service = s.Service.Copy()
				if fn == nil || (fn != nil && fn(mockTester{}, service)) {
					return
				}
			}

		case <-time.After(getWatchTimeout(expectTimeout)):
			if !expectTimeout {
				if service != nil && fn != nil {
					fn(t, service)
				}

				t.Fatalf("timed out before watchService found expected service state\n stack = %s", string(debug.Stack()))
			}

			return
		}
	}
}

func watchTask(t *testing.T, s *store.MemoryStore, watch chan events.Event, expectTimeout bool, fn func(t assert.TestingT, s *store.MemoryStore, n *api.Task) bool) {
	for {
		var task *api.Task
		select {
		case event := <-watch:
			if t, ok := event.(state.EventUpdateTask); ok {
				task = t.Task.Copy()
				if fn == nil || (fn != nil && fn(mockTester{}, s, task)) {
					return
				}
			}

			if t, ok := event.(state.EventDeleteTask); ok {
				task = t.Task.Copy()
				if fn == nil || (fn != nil && fn(mockTester{}, s, task)) {
					return
				}
			}

		case <-time.After(getWatchTimeout(expectTimeout)):
			if !expectTimeout {
				if task != nil && fn != nil {
					fn(t, s, task)
				}

				t.Fatalf("timed out before watchTask found expected task state %s", debug.Stack())
			}

			return
		}
	}
}
