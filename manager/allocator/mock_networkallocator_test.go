package allocator

import (
	"fmt"
	"maps"
	"slices"
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
type mockNetworkProvider struct {
	// failAllocate maps a network ID to the number of times the
	// allocators it creates fail to allocate that network before
	// succeeding.
	failAllocate map[string]int
}

var _ networkallocator.Provider = mockNetworkProvider{}

func (np mockNetworkProvider) NewAllocator(*networkallocator.Config) (networkallocator.NetworkAllocator, error) {
	return &mockNetworkAllocator{
		failAllocate: maps.Clone(np.failAllocate),
		networks:     make(map[string]int),
		nextHost:     make(map[string]int),
		services:     make(map[string]struct{}),
		tasks:        make(map[string]struct{}),
		attachments:  make(map[[2]string]struct{}),
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
	// failAllocate maps a network ID to the number of times left that
	// Allocate fails for that network.
	failAllocate map[string]int
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
	if na.failAllocate[n.ID] > 0 {
		na.failAllocate[n.ID]--
		return fmt.Errorf("mock failure allocating network %s", n.ID)
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

// AllocateService gives each of s's virtual IPs an address. Like the real
// network allocator, it drops a virtual IP on a network which is neither
// allocated nor attached to s's tasks.
func (na *mockNetworkAllocator) AllocateService(s *api.Service) error {
	if s.Endpoint != nil {
		vips := s.Endpoint.VirtualIPs[:0]
		for _, vip := range s.Endpoint.VirtualIPs {
			_, allocated := na.networks[vip.NetworkID]
			attached := slices.ContainsFunc(s.Spec.Task.Networks, func(n *api.NetworkAttachmentConfig) bool {
				return n.Target == vip.NetworkID
			})
			if !allocated && !attached {
				continue
			}
			if err := na.allocateAddress(vip.NetworkID, &vip.Addr); err != nil {
				return err
			}
			vips = append(vips, vip)
		}
		s.Endpoint.VirtualIPs = vips
	}
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
		if err := na.allocateAttachment(a); err != nil {
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
	if err := na.allocateAttachment(a); err != nil {
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

// allocateAttachment gives a an address on its network unless it already
// has one.
func (na *mockNetworkAllocator) allocateAttachment(a *api.NetworkAttachment) error {
	var addr string
	if len(a.Addresses) > 0 {
		addr = a.Addresses[0]
	}
	if err := na.allocateAddress(a.Network.ID, &addr); err != nil {
		return err
	}
	a.Addresses = []string{addr}
	return nil
}

// allocateAddress sets *addr to the next address on network nwID unless it
// is already set.
func (na *mockNetworkAllocator) allocateAddress(nwID string, addr *string) error {
	subnet, ok := na.networks[nwID]
	if !ok {
		return fmt.Errorf("network %s is not allocated", nwID)
	}
	if *addr == "" {
		na.nextHost[nwID]++
		*addr = fmt.Sprintf("10.0.%d.%d/24", subnet, na.nextHost[nwID]+1)
	}
	return nil
}
