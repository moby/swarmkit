package network

import (
	"fmt"

	"github.com/docker/docker/pkg/plugingetter"
	"github.com/docker/libnetwork/drvregistry"

	// the allocator types
	"github.com/docker/swarmkit/manager/allocator/network/driver"
	"github.com/docker/swarmkit/manager/allocator/network/errors"
	"github.com/docker/swarmkit/manager/allocator/network/ipam"
	"github.com/docker/swarmkit/manager/allocator/network/port"

	"github.com/docker/swarmkit/api"
)

type Allocator interface {
	Restore([]*api.Network, []*api.Service, []*api.Task, []*api.Node) error

	AllocateNetwork(*api.Network) error
	DeallocateNetwork(*api.Network) error

	AllocateService(*api.Service) error
	DeallocateService(*api.Service) error

	AllocateTask(*api.Task) error
	DeallocateTask(*api.Task) error

	AllocateNode(*api.Node, map[string]struct{}) error
	DeallocateNode(*api.Node) error
}

type allocator struct {
	// in order to figure out if the dependencies of a task are fulfilled, we
	// need to keep track of what we have allocated already. this also allows
	// us to avoid having to pass an endpoint from the service when allocating
	// a task.
	services map[string]*api.Service
	// note that we don't need to keep track of networks; the lower-level
	// components handle networks directly and keep track of them as needed,
	// unlike services, which exist strictly at this level and above.

	// also attachments don't need to be kept track of, because nothing depends
	// on them.

	ipam   ipam.Allocator
	driver driver.Allocator
	port   port.Allocator

	// ingressID is the ID of the ingress network. If it is empty, no ingress
	// network exists.
	ingressID string
}

// newAllocatorWithComponents creates a new allocator using the provided
// subcomponents. It's use is for testing, so that mocked subcomponents can be
// swapped in, and the driver initialization code can be skipped, in testing
// environments
func newAllocatorWithComponents(ipamAlloc ipam.Allocator, driverAlloc driver.Allocator, portAlloc port.Allocator) *allocator {
	return &allocator{
		services: map[string]*api.Service{},
		ipam:     ipamAlloc,
		driver:   driverAlloc,
		port:     portAlloc,
	}
}

// NewAllocator creates and returns a new, ready-to use allocator for all
// network resources. Before it can be used, the caller must call Restore with
// any existing objects that need to be restored to create the state
func NewAllocator(pg plugingetter.PluginGetter) Allocator {
	// NOTE(dperny): the err return value is currently not used in
	// drvregistry.New function. I get that it's very frowned upon to rely on
	// implementation details like that, but it simplifies the allocator enough
	// that i'm willing to just check it and panic if it occurs.
	reg, err := drvregistry.New(nil, nil, nil, nil, pg)
	if err != nil {
		panic("drvregistry.New returned an error... it's not supposed to do that")
	}
	// while we have access to a real DrvRegistry object, because this is the
	// only place we need it, let's init the drivers. If this fails, it means
	// the whole system is megascrewed
	for _, init := range initializers {
		if err := reg.AddDriver(init.ntype, init.fn, nil); err != nil {
			panic(fmt.Sprintf("reg.AddDriver returned an error: %v", err))
		}
	}

	// then, initialize the IPAM drivers
	if err := initIPAMDrivers(reg); err != nil {
		panic(fmt.Sprintf("initIPAMDrivers returned an error: %v", err))
	}
	return &allocator{
		services: map[string]*api.Service{},
		port:     port.NewAllocator(),
		ipam:     ipam.NewAllocator(reg),
		driver:   driver.NewAllocator(reg),
	}
}

// Restore takes slices of the object types managed by the network allocator
// and syncs the local state of the Allocator to match the state of the objects
// provided. It also initializes the default drivers to the reg.
//
// If an error occurs during the restore, the local state may be inconsistent,
// and this allocator should be abandoned
func (a *allocator) Restore(networks []*api.Network, services []*api.Service, tasks []*api.Task, nodes []*api.Node) error {
	// find if we have an ingress network in this list. if so, save its ID. we
	// need it to correctly allocate tasks and services. there should only ever
	// be 1 ingress network
	for _, nw := range networks {
		// ingress networks should have the ingress field set on the spec
		if nw.Spec.Ingress {
			a.ingressID = nw.ID
		}

		// howver, some older networks indicate that they're ingress with
		// labels.
		_, ok := nw.Spec.Annotations.Labels["com.docker.swarm.internal"]
		if ok && nw.Spec.Annotations.Name == "ingress" {
			a.ingressID = nw.ID
		}
	}

	endpoints := make([]*api.Endpoint, 0, len(services))
	for _, service := range services {
		// even if everything is empty, if the service is fully allocated, it
		// should be tracked.
		if a.isServiceFullyAllocated(service) {
			a.services[service.ID] = service
		}
		// nothing to do if we have a nil endpoint
		if service.Endpoint == nil {
			continue
		}
		endpoints = append(endpoints, service.Endpoint)
	}

	attachments := []*api.NetworkAttachment{}
	// get all of the attachments out of tasks
	for _, task := range tasks {
		for _, attachment := range task.Networks {
			attachments = append(attachments, attachment)
		}
	}
	for _, node := range nodes {
		for _, attachment := range node.Attachments {
			attachments = append(attachments, attachment)
		}
	}

	// now restore the various components
	// port can never error.
	a.port.Restore(endpoints)
	// errors from deeper components are always structured and can be returned
	// directly.
	if err := a.ipam.Restore(networks, endpoints, attachments); err != nil {
		return err
	}
	if err := a.driver.Restore(networks); err != nil {
		return err
	}
	return nil
}

// Allocate network takes the given network and allocates it to match the
// provided network spec
func (a *allocator) AllocateNetwork(n *api.Network) error {
	// first, figure out if the network is node-local, so we know whether or
	// not to run the IPAM allocator
	local, err := a.driver.IsNetworkNodeLocal(n)
	if err != nil {
		return err
	}
	if !local {
		// if the network is already allocated and we try to call allocate
		// again, ipam.AllocateNetwork will return ErrAlreadyAllocated, so we
		// don't need to check that at this level
		if err := a.ipam.AllocateNetwork(n); err != nil {
			return err
		}
	}
	if err := a.driver.Allocate(n); err != nil {
		a.ipam.DeallocateNetwork(n)
		return err
	}
	return nil
}

func (a *allocator) DeallocateNetwork(n *api.Network) error {
	// we don't need to worry about whether or not the network is node-local
	// for deallocation because it won't have ipam data anyway
	if err := a.driver.Deallocate(n); err != nil {
		return err
	}
	a.ipam.DeallocateNetwork(n)
	return nil
}

func (a *allocator) AllocateService(service *api.Service) error {
	// first, check if we have already allocated this service. Do this by
	// checking the service map for the service. Then, if it exists, check if
	// the spec version is the same.
	//
	// we only update the services map entry with the newer service version if
	// allocation succeeds, so if the spec version hasn't changed, then the
	// service hasn't changed.
	if oldService, ok := a.services[service.ID]; ok {
		var oldVersion, newVersion uint64
		// we need to do this dumb dance because for some crazy reason
		// SpecVersion is nullable
		if oldService.SpecVersion != nil {
			oldVersion = oldService.SpecVersion.Index
		}
		if service.SpecVersion != nil {
			newVersion = service.SpecVersion.Index
		}
		if oldVersion == newVersion {
			return errors.ErrAlreadyAllocated()
		}
	}
	// then, even if the spec has changed, check if the service is already
	// fully allocated. If so, then just update our local definition of the
	// service (so next time if it hasn't changed we can get it by map entry)
	// and return.
	if a.isServiceFullyAllocated(service) {
		a.services[service.ID] = service
		return errors.ErrAlreadyAllocated()
	}
	// handle the cases where service bits are nil
	endpoint := service.Endpoint
	if endpoint == nil {
		endpoint = &api.Endpoint{}
	}
	endpointSpec := service.Spec.Endpoint
	if endpointSpec == nil {
		endpointSpec = &api.EndpointSpec{}
	}
	proposal, err := a.port.Allocate(endpoint, service.Spec.Endpoint)
	if err != nil {
		return err
	}

	// TODO(dperny) this handles the case of spec.Networks, which we should
	// deprecate before removing this code entirely
	networks := service.Spec.Task.Networks
	if len(service.Spec.Task.Networks) == 0 && len(service.Spec.Networks) != 0 {
		networks = service.Spec.Networks
	}
	ids := make([]string, 0, len(networks))
	// build up a list of network ids to allocate vips for
	for _, nw := range networks {
		ids = append(ids, nw.Target)
	}

	// ingress is special because it cannot be normally attached to and so will
	// not be found in the spec's NetworkAttachmentConfigs. however, in the
	// actual objects, it should have a VIP. so, if we need it, append it to
	// the list of network IDs we're requesting VIPs for.
	if ingressNeeded(proposal.Ports()) {
		ids = append(ids, a.ingressID)
	}

	if err := a.ipam.AllocateVIPs(endpoint, ids); err != nil {
		// if the error is a result of anything other than the fact that we're
		// already allocated, return it
		if !errors.IsErrAlreadyAllocated(err) {
			return err
		}
	}
	// commit the port allocation, update the services map entry, and return.
	//
	// if both the VIPs _and_ the ports were already fully allocated, we would
	// have returned ErrAlreadyAllocated up above.
	proposal.Commit()
	service.Endpoint = endpoint
	service.Endpoint.Ports = proposal.Ports()
	service.Endpoint.Spec = endpointSpec
	a.services[service.ID] = service

	return nil
}

func (a *allocator) DeallocateService(service *api.Service) error {
	if service.Endpoint != nil {
		a.port.Deallocate(service.Endpoint)
		a.ipam.DeallocateVIPs(service.Endpoint)
	}
	return nil
}

func (a *allocator) AllocateTask(task *api.Task) error {
	// if the task state is past new, then it's already allocated
	if task.Status.State > api.TaskStateNew {
		return errors.ErrAlreadyAllocated()
	}
	// if the task has an empty service ID, it doesn't depend on the service
	// being allocated. It also will not have an endpoint.
	if task.ServiceID != "" {
		service, ok := a.services[task.ServiceID]
		if !ok {
			return errors.ErrDependencyNotAllocated("service", task.ServiceID)
		}
		// set the task endpoint to match the service endpoint
		task.Endpoint = service.Endpoint
	}
	// check if the task may need to be attached to the ingress network.
	// ingress is special because it cannot be attached to normally, and so
	// will not be in the spec's NetworkAttachmentConfigs. however, if it is
	// required, there needs to be a NetworkAttachment on the object for the
	// ingress network.
	attachmentConfigs := task.Spec.Networks
	if ingressNeeded(task.Endpoint.Ports) {
		// NOTE(dperny): if i recall correctly, append should not modify the
		// original slice here, which means this is safe to do without
		// accidentally modifying the spec.
		attachmentConfigs = append(attachmentConfigs,
			// we only need to provide the ingress ID as the target in a
			// network attachment config.
			&api.NetworkAttachmentConfig{Target: a.ingressID},
		)
	}
	// typically, we would have to pass both the configs and the actual objects
	// in order to reconile the differences. however, tasks are a 1-way street;
	// once they're allocated, they're done.
	attachments, err := a.ipam.AllocateAttachments(attachmentConfigs)
	if err != nil {
		return err
	}
	task.Networks = attachments
	return nil
}

func (a *allocator) DeallocateTask(task *api.Task) error {
	a.ipam.DeallocateAttachments(task.Networks)
	for _, attachment := range task.Networks {
		// remove the addresses after we've deallocated every attachment
		attachment.Addresses = nil
	}
	return nil
}

// AllocateNode allocates the network attachments for a node. The second
// argument, a set of networks, is used to indicate which networks the node
// needs to be attached to. This is necessary because the node's attachments
// are informed by its task allocations, which is a list not available in this
// context.
//
// The passed networks map will be mutated, and should not be reused after
// passing to this function.
func (a *allocator) AllocateNode(node *api.Node, networks map[string]struct{}) error {
	// before we do anything, add the ingress network if it exists to the
	// networks map. we always need an ingress network attachment.
	if a.ingressID != "" {
		// if for some reason, the caller has already added the ingress network
		// to the networks list, this will do nothing, which isn't a problem.
		networks[a.ingressID] = struct{}{}
	}

	// first, figure out which networks we keep and which we throw away from
	// this node
	var keep, remove []*api.NetworkAttachment
	// do this by going through the current attachments, and checking if the
	// network is in our list of desired networks. If so, add it to keep. If
	// not, add it to remove.
	for _, attachment := range node.Attachments {
		if _, ok := networks[attachment.Network.ID]; ok {
			keep = append(keep, attachment)
			// remove the network from the set tracking our desired networks,
			// because it is already fully allocated
			delete(networks, attachment.Network.ID)
		} else {
			remove = append(remove, attachment)
		}
	}

	// you may ask, shouldn't we deallocate first, to free up resources? the
	// answer is no. because each network has a discrete pool from which it
	// allocates addresses, the addresses from one network will never be
	// available for allocation in another network, and deallocating first
	// would have no benefit. In addition, deallocating first would mean a
	// failed allocation would force us to re-allocate everything we'd just
	// dropped.

	// at this point, any entries remaining in the networks are not yet
	// allocated. we can build a list of network attachment configs to pass
	// into AllocateAttachments
	allocate := make([]*api.NetworkAttachmentConfig, 0, len(networks))
	for nwid := range networks {
		allocate = append(allocate, &api.NetworkAttachmentConfig{
			Target: nwid,
		})
	}

	// finally, try allocating the attachments. if it fails return an error. If
	// it succeeds, then free the attachments we no longer need, and set the
	// node attachments list
	attachments, err := a.ipam.AllocateAttachments(allocate)
	if err != nil {
		return err
	}

	a.ipam.DeallocateAttachments(remove)
	node.Attachments = append(keep, attachments...)
	return nil
}

func (a *allocator) DeallocateNode(node *api.Node) error {
	a.ipam.DeallocateAttachments(node.Attachments)
	for _, attachment := range node.Attachments {
		attachment.Addresses = nil
	}
	return nil
}

// IsServiceFullyAllocated takes a service and returns true if its endpoint
// matches its spec and there is no allocation required.
func (a *allocator) isServiceFullyAllocated(service *api.Service) bool {
	// this is kind of tricky... we need to figure out which service
	// endpoints are fully allocated or not here so we can add the fully
	// allocated ones to the services map. note that even if a service
	// isn't fully allocated, we still need to pass it to the Restore
	// methods, because we absolutely must have the entire state, fully
	// allocated or not, before we can pursue new allocations.
	if service.Spec.Endpoint != nil && service.Endpoint.Spec != nil {
		// if the mode differs, the service isn't fully allocated
		if service.Endpoint.Spec.Mode != service.Spec.Endpoint.Mode {
			return false
		}
		// if we're using vips, check that we're using the right vips
		if service.Spec.Endpoint.Mode == api.ResolutionModeVirtualIP {
			// how many networks should we have? the same number as our spec's
			// attachments, plus ingress if we need it. ingress cannot be
			// attached to normally, so will not be in the spec's network
			// attachments, but if it is needed it should be in the service's
			// VIPs.
			expectedNetworks := len(service.Spec.Task.Networks)
			ingress := ingressNeeded(service.Spec.Endpoint.Ports)
			if ingress {
				expectedNetworks = expectedNetworks + 1
			}
			// if there are differing numbers of VIPs and attachments, we have
			// some allocation or deallocation to do
			if len(service.Endpoint.VirtualIPs) != expectedNetworks {
				return false
			}
			// i'm not totally happy with this part because I think it slightly
			// breaks the separation of concerns, but i think it's more
			// important to guard the ipam package from the details of services
			// and tasks than to guard the network allocator package from the
			// details of IP addresses.
		vipsLoop:
			for _, vip := range service.Endpoint.VirtualIPs {
				// first, if we need ingress, check if this VIP is for ingress.
				// if it is, then it won't be in the spec's networks, but it is
				// supposed to be there, so we can skip looking for it. If
				// ingress ISN'T needed but we find it in the VIPs, then it
				// will fall through this case, pass through the spec's
				// networks loop without continuing, and then return false
				// because it's not supposed to be there.
				if ingress && vip.NetworkID == a.ingressID {
					continue vipsLoop
				}
				// NOTE(dperny): this does _not_ cover the deprecated
				// service.Spec.Networks field.
				for _, nw := range service.Spec.Task.Networks {
					if nw != nil && nw.Target == vip.NetworkID {
						// if we find a target that matches this vip, then
						// we can go to the next VIP and check it
						continue vipsLoop
					}
				}
				// if we get all the way through the networks and there is
				// nothing matching this VIP, the service isn't fully
				// allocated
				return false
			}
		}
		// if we got this far, and the ports are also already allocated,
		// then the service is fully allocated and we can track it in our
		// map.
		if !port.AlreadyAllocated(service.Endpoint, service.Spec.Endpoint) {
			return false
		}
	}
	return true
}

// ingressNeeded checks the port list, and returns true if the ingress network
// is needed. the ingress network is needed if there is at least 1 port in the
// port configs that is in PublishModeIngress.
func ingressNeeded(ports []*api.PortConfig) bool {
	for _, port := range ports {
		if port.PublishMode == api.PublishModeIngress {
			return true
		}
	}
	return false
}
