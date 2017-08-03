package cniallocator

import (
	"fmt"

	"github.com/docker/swarmkit/api"
	"github.com/docker/swarmkit/manager/network"
	"github.com/pkg/errors"
)

type cniNetworkAllocator struct {
	// Allocated networks
	networks map[string]*api.Network

	// We track these solely in order to correctly answer
	// "Is*Allocated" and prevent repeated attempts to do nop
	// allocations
	tasks map[string]struct{}
	nodes map[string]struct{}
}

// New returns a new Allocator handle
func New() (network.Allocator, error) {
	na := &cniNetworkAllocator{
		networks: make(map[string]*api.Network),
		tasks:    make(map[string]struct{}),
		nodes:    make(map[string]struct{}),
	}

	return na, nil
}

// Allocate allocates all the necessary resources both general
// and driver-specific which may be specified in the NetworkSpec
func (na *cniNetworkAllocator) Allocate(n *api.Network) error {
	if _, ok := na.networks[n.ID]; ok {
		return fmt.Errorf("network %s already allocated", n.ID)
	}

	if n.Spec.DriverConfig == nil {
		return fmt.Errorf("CNI network %s must have DriverConfig", n.ID)
	}

	if n.Spec.IPAM != nil {
		return fmt.Errorf("CNI network %s cannot have IPAM config", n.ID)
	}

	options := make(map[string]string)
	// reconcile the driver specific options from the network spec
	// and from the operational state retrieved from the store
	// TODO(ijc) lifted right from networkallocator.allocateDriverState.
	// Find a common home
	if n.Spec.DriverConfig != nil {
		for k, v := range n.Spec.DriverConfig.Options {
			options[k] = v
		}
	}
	if n.DriverState != nil {
		for k, v := range n.DriverState.Options {
			options[k] = v
		}
	}
	n.DriverState = &api.Driver{
		Name:    n.Spec.DriverConfig.Name,
		Options: options,
	}

	na.networks[n.ID] = n

	return nil
}

// Deallocate frees all the general and driver specific resources
// which were assigned to the passed network.
func (na *cniNetworkAllocator) Deallocate(n *api.Network) error {
	delete(na.networks, n.ID)
	return nil
}

// AllocateService allocates all the network resources such as virtual
// IP and ports needed by the service.
func (na *cniNetworkAllocator) AllocateService(s *api.Service) (err error) {
	return errors.New("CNI AllocateService should never be called")
}

// DeallocateService de-allocates all the network resources such as
// virtual IP and ports associated with the service.
func (na *cniNetworkAllocator) DeallocateService(s *api.Service) error {
	// Nop
	return nil
}

// IsAllocated returns if the passed network has been allocated or not.
func (na *cniNetworkAllocator) IsAllocated(n *api.Network) bool {
	_, ok := na.networks[n.ID]
	return ok
}

// IsTaskAllocated returns if the passed task has its network resources allocated or not.
func (na *cniNetworkAllocator) IsTaskAllocated(t *api.Task) bool {
	_, ok := na.tasks[t.ID]
	return ok
}

// HostPublishPortsNeedUpdate returns true if the passed service needs
// allocations for its published ports in host (non ingress) mode
func (na *cniNetworkAllocator) HostPublishPortsNeedUpdate(s *api.Service) bool {
	return false
}

// IsServiceAllocated returns false if the passed service needs to have network resources allocated/updated.
func (na *cniNetworkAllocator) IsServiceAllocated(s *api.Service, flags ...func(*network.ServiceAllocationOpts)) bool {
	// No service ever needs allocation for CNI
	return true
}

// IsNodeAllocated returns if the passed node has its network resources allocated or not.
func (na *cniNetworkAllocator) IsNodeAllocated(node *api.Node) bool {
	_, ok := na.nodes[node.ID]
	return ok
}

// AllocateNode allocates the IP addresses for the network to which
// the node is attached.
func (na *cniNetworkAllocator) AllocateNode(node *api.Node) error {
	na.nodes[node.ID] = struct{}{}
	return nil
}

// DeallocateNode deallocates the IP addresses for the network to
// which the node is attached.
func (na *cniNetworkAllocator) DeallocateNode(node *api.Node) error {
	delete(na.nodes, node.ID)
	return nil
}

// AllocateTask allocates all the endpoint resources for all the
// networks that a task is attached to.
func (na *cniNetworkAllocator) AllocateTask(t *api.Task) error {
	na.tasks[t.ID] = struct{}{}
	return nil
}

// DeallocateTask releases all the endpoint resources for all the
// networks that a task is attached to.
func (na *cniNetworkAllocator) DeallocateTask(t *api.Task) error {
	delete(na.tasks, t.ID)
	return nil
}
