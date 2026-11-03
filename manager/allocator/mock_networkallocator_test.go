package allocator

import (
	"fmt"
	"testing"

	"github.com/moby/swarmkit/v2/api"
	"github.com/moby/swarmkit/v2/manager/allocator/networkallocator"
)

// TestNodeAllocatorIngressMock runs the allocator test suite's ingress node
// attachment tests against mockNetworkProvider, as the full suite only runs
// with a real network allocator provider.
func TestNodeAllocatorIngressMock(t *testing.T) {
	for name, test := range map[string]func(*testSuite){
		"Replaced": (*testSuite).TestNodeAllocatorIngressReplaced,
		"Created":  (*testSuite).TestNodeAllocatorIngressCreated,
	} {
		t.Run(name, func(t *testing.T) {
			s := &testSuite{np: mockNetworkProvider{}}
			s.SetT(t)
			test(s)
		})
	}
}

// mockNetworkProvider is a [networkallocator.Provider] for
// mockNetworkAllocator.
type mockNetworkProvider struct{}

var _ networkallocator.Provider = mockNetworkProvider{}

func (mockNetworkProvider) NewAllocator(*networkallocator.Config) (networkallocator.NetworkAllocator, error) {
	return &mockNetworkAllocator{
		networks:    make(map[string]int),
		nextHost:    make(map[string]int),
		services:    make(map[string]struct{}),
		tasks:       make(map[string]struct{}),
		attachments: make(map[[2]string]struct{}),
	}, nil
}

func (mockNetworkProvider) PredefinedNetworks() []networkallocator.PredefinedNetworkData {
	return nil
}

func (mockNetworkProvider) SetDefaultVXLANUDPPort(uint32) error            { return nil }
func (mockNetworkProvider) ValidateNetworkDriver(*api.Driver) error        { return nil }
func (mockNetworkProvider) ValidateIngressNetworkDriver(*api.Driver) error { return nil }
func (mockNetworkProvider) ValidateIPAMDriver(*api.Driver) error           { return nil }

// mockNetworkAllocator is an in-memory [networkallocator.NetworkAllocator]
// which allocates every network as an overlay network with its own /24
// subnet in 10.0.0.0/8, and hands out addresses from it in sequence. It
// keeps addresses which are already set, so that the allocator can restore
// its state at init.
type mockNetworkAllocator struct {
	// networks maps the ID of each allocated network to its subnet number.
	networks   map[string]int
	nextSubnet int
	// nextHost maps a network ID to the last host number allocated on it.
	nextHost map[string]int
	services map[string]struct{}
	tasks    map[string]struct{}
	// attachments is the set of allocated node attachments, by node ID
	// and network ID.
	attachments map[[2]string]struct{}
}

var _ networkallocator.NetworkAllocator = (*mockNetworkAllocator)(nil)

func (na *mockNetworkAllocator) IsAllocated(n *api.Network) bool {
	_, ok := na.networks[n.ID]
	return ok
}

func (na *mockNetworkAllocator) Allocate(n *api.Network) error {
	if na.IsAllocated(n) {
		return fmt.Errorf("network %s is already allocated", n.ID)
	}
	na.nextSubnet++
	na.networks[n.ID] = na.nextSubnet
	if n.DriverState == nil {
		n.DriverState = &api.Driver{Name: "overlay"}
	}
	if n.IPAM == nil {
		n.IPAM = &api.IPAMOptions{
			Configs: []*api.IPAMConfig{{
				Subnet:  fmt.Sprintf("10.0.%d.0/24", na.nextSubnet),
				Gateway: fmt.Sprintf("10.0.%d.1", na.nextSubnet),
			}},
		}
	}
	return nil
}

func (na *mockNetworkAllocator) Deallocate(n *api.Network) error {
	delete(na.networks, n.ID)
	return nil
}

func (na *mockNetworkAllocator) IsServiceAllocated(s *api.Service, _ ...func(*networkallocator.ServiceAllocationOpts)) bool {
	_, ok := na.services[s.ID]
	return ok
}

func (na *mockNetworkAllocator) AllocateService(s *api.Service) error {
	na.services[s.ID] = struct{}{}
	return nil
}

func (na *mockNetworkAllocator) DeallocateService(s *api.Service) error {
	delete(na.services, s.ID)
	return nil
}

func (na *mockNetworkAllocator) IsTaskAllocated(t *api.Task) bool {
	_, ok := na.tasks[t.ID]
	return ok
}

func (na *mockNetworkAllocator) AllocateTask(t *api.Task) error {
	for _, a := range t.Networks {
		if err := na.allocateAddress(a); err != nil {
			return err
		}
	}
	na.tasks[t.ID] = struct{}{}
	return nil
}

func (na *mockNetworkAllocator) DeallocateTask(t *api.Task) error {
	delete(na.tasks, t.ID)
	return nil
}

func (na *mockNetworkAllocator) AllocateAttachment(node *api.Node, a *api.NetworkAttachment) error {
	if err := na.allocateAddress(a); err != nil {
		return err
	}
	na.attachments[[2]string{node.ID, a.Network.ID}] = struct{}{}
	return nil
}

func (na *mockNetworkAllocator) DeallocateAttachment(node *api.Node, a *api.NetworkAttachment) error {
	delete(na.attachments, [2]string{node.ID, a.Network.ID})
	return nil
}

func (na *mockNetworkAllocator) IsAttachmentAllocated(node *api.Node, a *api.NetworkAttachment) bool {
	_, ok := na.attachments[[2]string{node.ID, a.Network.ID}]
	return ok
}

// allocateAddress gives a an address on its network unless it already has
// one.
func (na *mockNetworkAllocator) allocateAddress(a *api.NetworkAttachment) error {
	subnet, ok := na.networks[a.Network.ID]
	if !ok {
		return fmt.Errorf("network %s is not allocated", a.Network.ID)
	}
	if len(a.Addresses) == 0 {
		na.nextHost[a.Network.ID]++
		a.Addresses = []string{fmt.Sprintf("10.0.%d.%d/24", subnet, na.nextHost[a.Network.ID]+1)}
	}
	return nil
}
