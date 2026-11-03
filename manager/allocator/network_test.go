package allocator

import (
	"testing"
	"time"

	"github.com/moby/swarmkit/v2/api"
	"github.com/moby/swarmkit/v2/manager/state"
	"github.com/moby/swarmkit/v2/manager/state/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpdatePortsInHostPublishMode(t *testing.T) {
	service := api.Service{
		Spec: api.ServiceSpec{
			Endpoint: &api.EndpointSpec{
				Ports: []*api.PortConfig{
					{
						Protocol:      api.ProtocolTCP,
						TargetPort:    80,
						PublishedPort: 10000,
						PublishMode:   api.PublishModeHost,
					},
				},
			},
		},
		Endpoint: &api.Endpoint{
			Ports: []*api.PortConfig{
				{
					Protocol:      api.ProtocolTCP,
					TargetPort:    80,
					PublishedPort: 15000,
					PublishMode:   api.PublishModeHost,
				},
			},
		},
	}
	updatePortsInHostPublishMode(&service)

	assert.Equal(t, len(service.Endpoint.Ports), 1)
	assert.Equal(t, service.Endpoint.Ports[0].PublishedPort, uint32(10000))
	assert.Equal(t, service.Endpoint.Spec.Ports[0].PublishedPort, uint32(10000))
}

func ingressService(id string) *api.Service {
	return &api.Service{
		ID: id,
		Spec: api.ServiceSpec{
			Annotations: api.Annotations{Name: id},
			Endpoint: &api.EndpointSpec{
				Ports: []*api.PortConfig{{
					Protocol:      api.ProtocolTCP,
					TargetPort:    80,
					PublishedPort: 8080,
					PublishMode:   api.PublishModeIngress,
				}},
			},
		},
	}
}

// TestIngressAllocationRetried checks that nodes, services and tasks are
// attached to an ingress network created after the allocator started, once a
// retry of its failed allocation succeeds.
func TestIngressAllocationRetried(t *testing.T) {
	s := store.NewMemoryStore(nil)
	defer s.Close()

	suite := &testSuite{np: mockNetworkProvider{failAllocate: map[string]int{"ingress": 1}}}
	suite.SetT(t)
	a := suite.newAllocator(s)

	node1 := &api.Node{ID: "nodeID1"}
	node2 := &api.Node{ID: "nodeID2"}
	require.NoError(t, s.Update(func(tx store.Tx) error {
		for _, id := range []string{"overlay1", "overlay2"} {
			require.NoError(t, store.CreateNetwork(tx, &api.Network{
				ID:   id,
				Spec: api.NetworkSpec{Annotations: api.Annotations{Name: id}},
			}))
		}
		require.NoError(t, store.CreateNode(tx, node1))
		return store.CreateNode(tx, node2)
	}))

	netWatch, cancel := state.Watch(s.WatchQueue(), api.EventUpdateNetwork{}, api.EventDeleteNetwork{})
	defer cancel()

	defer suite.startAllocator(a)()

	// The allocator commits overlay1 and overlay2 at init, after it has
	// looked for an ingress network.
	watchNetwork(t, netWatch, false, isValidNetwork)
	watchNetwork(t, netWatch, false, isValidNetwork)

	// The first attempt to allocate the ingress network fails, so the
	// service and its task cannot be allocated either.
	require.NoError(t, s.Update(func(tx store.Tx) error {
		require.NoError(t, store.CreateNetwork(tx, ingressNetwork("ingress")))
		require.NoError(t, store.CreateService(tx, ingressService("service1")))
		return store.CreateTask(tx, &api.Task{
			ID:           "task1",
			ServiceID:    "service1",
			NodeID:       node2.ID,
			DesiredState: api.TaskStateRunning,
			Spec: api.TaskSpec{
				Networks: []*api.NetworkAttachmentConfig{{Target: "overlay2"}},
			},
		})
	}))

	// Deleting a network makes the allocator retry failed allocations.
	require.NoError(t, s.Update(func(tx store.Tx) error {
		return store.DeleteNetwork(tx, "overlay1")
	}))
	watchNetwork(t, netWatch, false, func(t assert.TestingT, n *api.Network) bool {
		return n.ID == "ingress" && isValidNetwork(t, n)
	})

	waitNodeAttachments(t, s, node1, []string{"ingress"})
	waitNodeAttachments(t, s, node2, []string{"ingress", "overlay2"})
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		var (
			svc  *api.Service
			task *api.Task
		)
		s.View(func(tx store.ReadTx) {
			svc = store.GetService(tx, "service1")
			task = store.GetTask(tx, "task1")
		})
		if assert.NotNil(c, svc.Endpoint) && assert.Len(c, svc.Endpoint.VirtualIPs, 1) {
			assert.Equal(c, "ingress", svc.Endpoint.VirtualIPs[0].NetworkID)
			assert.NotEmpty(c, svc.Endpoint.VirtualIPs[0].Addr)
		}
		if assert.Equal(c, api.TaskStatePending, task.Status.State) {
			var networks []string
			for _, na := range task.Networks {
				assert.Len(c, na.Addresses, 1)
				networks = append(networks, na.Network.ID)
			}
			assert.ElementsMatch(c, []string{"ingress", "overlay2"}, networks)
		}
	}, getWatchTimeout(false), 10*time.Millisecond)
}

// TestIngressAllocationRetriedAtInit checks that nodes are attached to the
// ingress network as allocated, when the allocator fails to allocate it at
// the start of init and succeeds later in init.
func TestIngressAllocationRetriedAtInit(t *testing.T) {
	s := store.NewMemoryStore(nil)
	defer s.Close()

	suite := &testSuite{np: mockNetworkProvider{failAllocate: map[string]int{"ingress": 1}}}
	suite.SetT(t)
	a := suite.newAllocator(s)

	node1 := &api.Node{ID: "nodeID1"}
	require.NoError(t, s.Update(func(tx store.Tx) error {
		require.NoError(t, store.CreateNetwork(tx, ingressNetwork("ingress")))
		return store.CreateNode(tx, node1)
	}))

	netWatch, cancel := state.Watch(s.WatchQueue(), api.EventUpdateNetwork{}, api.EventDeleteNetwork{})
	defer cancel()

	defer suite.startAllocator(a)()

	watchNetwork(t, netWatch, false, isValidNetwork) // ingress
	waitNodeAttachments(t, s, node1, []string{"ingress"})

	var (
		ingress *api.Network
		node    *api.Node
	)
	s.View(func(tx store.ReadTx) {
		ingress = store.GetNetwork(tx, "ingress")
		node = store.GetNode(tx, node1.ID)
	})
	require.Len(t, node.Attachments, 1)
	assert.Equal(t, ingress.DriverState, node.Attachments[0].Network.DriverState)
	assert.Equal(t, ingress.IPAM, node.Attachments[0].Network.IPAM)
}
