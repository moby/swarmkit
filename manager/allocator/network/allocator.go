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

// Allocator is a the highest level type that should read or write network
// resources. Allocator functions as an interface between the higher level
// caller, which is concerned with Services and Tasks, to lower-level
// components, which are concerned with Endpoints and Attachments.
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

	// some networks are node-local, meaning we do not perform driver or IP
	// address allocation on them. however, we still need to know what they are
	// so that the allocator can handle attachments to these node-local
	// networks correctly. this map keeps a set of all such node-local
	// networks, so we know when allocating attachments to skip over these.
	// This is roughly analogous to the networks map in the IPAM allocator but
	// with a different set of networks
	nodeLocalNetworks map[string]*api.Network

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
		services:          map[string]*api.Service{},
		nodeLocalNetworks: map[string]*api.Network{},
		ipam:              ipamAlloc,
		driver:            driverAlloc,
		port:              portAlloc,
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
		panic(fmt.Sprintf("drvregistry.New returned an error: %v", err))
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
		services:          map[string]*api.Service{},
		nodeLocalNetworks: map[string]*api.Network{},
		port:              port.NewAllocator(),
		ipam:              ipam.NewAllocator(reg),
		driver:            driver.NewAllocator(reg),
	}
}

// Restore takes slices of the object types managed by the network allocator
// and syncs the local state of the Allocator to match the state of the objects
// provided.
//
// If an error occurs during the restore, the local state may be inconsistent,
// and this allocator should be abandoned.
func (a *allocator) Restore(networks []*api.Network, services []*api.Service, tasks []*api.Task, nodes []*api.Node) error {
	// there is a problem with restoring nodes: because node deallocation
	// depends on network deallocation, it is possible for a network to be
	// deallocated but the corresponding deallocation of nodes to not occur.
	// this can lead to a situation on restore where a node has attachments
	// belonging to no network.
	//
	// the simple fix, first off, is to ignore attempts to deallocate an
	// attachment which has no corresponding network. this allows the caller to
	// get correct allocator and node state by simply reallocating all nodes
	// after restoring network state.
	//
	// however, Restore will return errors if it attempts to restore an
	// attachment for which the network is not allocated; specifically,
	// ipam's Restore will return ErrBadState and no restore any subsequent
	// (correct) allocations.
	//
	// to avoid this case, we'll keep a set of all network IDs that we've
	// encountered so far. then, when we compile our slice of attachments to
	// restore, we'll skip any node attachments belonging to a network not in
	// this list
	//
	// because networks cannot be updated, we can be sure that any node
	// attachment that exists either belongs to a fully allocated network, or
	// belongs to a deleted network, so we can put even unallocated networks in
	// this map.
	//
	// This issue does not exist with task attachments, because a network
	// cannot be deleted while it is still in use by tasks.
	existingNetworks := make(map[string]struct{}, len(networks))

	// find if we have an ingress network in this list. if so, save its ID. we
	// need it to correctly allocate tasks and services. there should only ever
	// be 1 ingress network
	for _, nw := range networks {
		existingNetworks[nw.ID] = struct{}{}

		if isIngress(nw) {
			a.ingressID = nw.ID
		}
		// TODO(dperny): this is broken. if the network is node local but not
		// yet allocated, it will be stuck. the allocation doesn't really mean
		// anything for a node-local network, but it does fill in driver name,
		// for example. maybe we should just allocate here?
		local, err := a.driver.IsNetworkNodeLocal(nw)
		if err != nil {
			return err
		}

		// check if the network is node-local, and add it to our set if so.
		if local {
			a.nodeLocalNetworks[nw.ID] = nw
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
		// skip all tasks that are in terminal states. their resources are no
		// longer in use
		if task.Status.State >= api.TaskStateCompleted {
			continue
		}
		for _, attachment := range task.Networks {
			// only restore attachments that do not belong to node-local
			// networks
			if _, ok := a.nodeLocalNetworks[attachment.Network.ID]; !ok {
				attachments = append(attachments, attachment)
			}
		}
	}

	for _, node := range nodes {
		// there will be no node-local networks in a node's attachments
		for _, attachment := range node.Attachments {
			// if we haven't seen this network while restoring networks, then
			// skip restoring this attachment; it must be deallocated
			if _, ok := existingNetworks[attachment.Network.ID]; ok {
				attachments = append(attachments, attachment)
			}
		}
		// TODO(dperny): remove code that handles the singular node attachment
		if node.Attachment != nil {
			attachments = append(attachments, node.Attachment)
		}
	}

	// now restore the various components
	// port can never error.
	a.port.Restore(endpoints)
	// errors from deeper components are always structured and can be returned
	// directly.

	// it is safe to call ipam.Restore on all networks including node-local
	// networks, because those networks have no ipam state and nothing to
	// restore
	if err := a.ipam.Restore(networks, endpoints, attachments); err != nil {
		return err
	}
	if err := a.driver.Restore(networks); err != nil {
		return err
	}
	return nil
}

// AllocateNetwork takes the given network and allocates it to match the
// provided network spec
func (a *allocator) AllocateNetwork(n *api.Network) error {
	// before allocating, check if this is an ingress network. if it is and we
	// already have an ingress network, return an error. However, if this
	// network IS the ingress network, keep going. This will likely lead us to
	// returning ErrAlreadyAllocated later on, but might also be the case if
	// the ingress network was found in the Restore but not fully allocated at
	// the time
	if isIngress(n) && a.ingressID != "" && a.ingressID != n.ID {
		return errors.ErrInvalidSpec("an ingress network (%v) is already allocated", a.ingressID)
	}
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
	// now that we've fully allocated, if this is an ingress network, set the
	// ingressID
	if isIngress(n) {
		a.ingressID = n.ID
	}
	return nil
}

// DeallocateNetwork takes a network object and releases its network resources
func (a *allocator) DeallocateNetwork(n *api.Network) error {
	// we don't need to worry about whether or not the network is node-local
	// for deallocation because it won't have ipam data anyway
	if err := a.driver.Deallocate(n); err != nil {
		return err
	}

	// if the network being removed is ingress, clear out the ingress ID.
	if n.ID == a.ingressID {
		a.ingressID = ""
	}

	a.ipam.DeallocateNetwork(n)
	return nil
}

// AllocateService takes a service and allocates its network resources to match
// the service's spec. If AllocateService returns an error, then all of the
// service's allocations will have been rolled back, and the caller can try
// again.
//
// If the result is nil, then no error has occurred, and the service is fully
// allocated and should be be committed. The service will either be fully
// allocated or not allocated at all. This method will never leave the service
// in a partially allocated state.
//
// If the service is already fully allocated, it will return ErrFullyAllocated.
// The service is valid, but the caller does not need to commit the results.
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
		if oldService.SpecVersion != nil {
			oldVersion = oldService.SpecVersion.Index
		}
		if service.SpecVersion != nil {
			newVersion = service.SpecVersion.Index
		}
		// If the version is older than what we already have, then the service
		// is fully allocated already
		if oldVersion >= newVersion {
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

	// Service allocation is the most complicated type, because it includes
	// operations (port.Allocate and ipam.AllocateVIPs) that can BOTH
	// allocate new resources, and free resources no longer needed.
	//
	// Allocating new resources is an operation that can fail, but any
	// allocations performed before one of them failed can be rolled back
	// easily by releasing them. However, deallocating resources cannot be
	// easily or reliably rolled back. Once resource are deallocated, some
	// other process could come along and scoop them out from under us. Once a
	// resource is freed, there's no guarantee that it can be re-acquired.
	// Fortunately freeing resources CANNOT fail (or, rather, it can, but only
	// in such a way that we crash everything anyway), which means once you
	// START freeing resources, you can assume that they will all be
	// successfully freed, and there is no reason to roll back.
	//
	// Therefore, because the operations can fail can be undone (allocation),
	// and the operations cannot be undone cannot fail (deallocation) then we
	// must allocate first, and only deallocate once all allocation succeeds.
	//
	// The problem arises specifically because there are TWO operations on
	// service, allocating ports and allocating VIPs, that can both acquire and
	// release resources. In an naive implementation, one might do something
	// like this:
	//
	//    Allocate ports
	//    > Allocate new ports
	//    > Free unused ports
	//    Allocate VIPs
	//    > Allocate new VIPs
	//    > Release unused VIPs
	//
	// Thus, if allocation of new vips fails, we will have already freed some
	// ports and are stuck in a state where we can't go forward and can't go
	// backward. To get around this, we have to disallow concurrent access to
	// one or the other of the port or ipam allocators. Because allocating
	// ports is likely a less common operation, and because the port allocator
	// has no external dependencies, it is the part that we choose to
	// single-thread. Further, because the rolling back logic of a port
	// allocation is rather complicated and likely to be error prone even if we
	// guarantee that we can reacquire released ports, we use a two-phase model
	// for allocating ports
	//
	// allocate ports first. calling port.Allocate doesn't actually alter the
	// state of the port allocator, it just returns a Proposal or an error. If
	// there is an error then port allocation is not possible, and we can stop
	// here. Otherwise, the returned proposal contains the information which is
	// needed to alter the state of the port allocator and commit the changes.
	// If a proposal is returned, then committing the proposal is guaranteed to
	// succeed.
	//
	// When the code in this function is unpacked, it looks (essentially) like
	// this:
	//
	//   Parts that can fail but be undone:
	//     port.Allocate: Allocate Ports (verifying if allocation _will_ succeed)
	//     ipam.AllocateVIPs: Allocate new VIPs
	//   Parts that cannot fail but cannot be undone:
	//     in the same call to ipam.AllocateVIPs: Deallocate VIPs no longer in use
	//     proposal.Commit: Commit the port changes (which we know to be valid)
	//
	// This complication does not exist in networks or tasks, which are only
	// one-way, and therefore only acquire resources or release them, never
	// both at the same time like services, and it does not exist on nodes
	// because nodes only worry about one resource type (attachments).
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
	ids := make(map[string]struct{}, len(networks))
	// build up a list of network ids to allocate vips for
	for _, nw := range networks {
		// we don't allocate VIPs for node-local networks, so if the network is
		// found in the nodeLocalNetworks set, skip it
		if _, ok := a.nodeLocalNetworks[nw.Target]; !ok {
			ids[nw.Target] = struct{}{}
		}
	}

	// ingress is special because it cannot be normally attached to and so will
	// not be found in the spec's NetworkAttachmentConfigs. however, in the
	// actual objects, it should have a VIP. so, if we need it, append it to
	// the list of network IDs we're requesting VIPs for.
	if ingressNeeded(proposal.Ports()) {
		if a.ingressID != "" {
			ids[a.ingressID] = struct{}{}
		} else {
			// if we need an ingress network, but we don't currently have one,
			// then the dependencies for this service are not allocated. if we
			// didn't return an error here, we would still get an
			// ErrDependencyNotAllocated, but it would be from the IPAM
			// allocator, complaining that network "" (empty string) was not
			// allocated.
			return errors.ErrDependencyNotAllocated("network", "ingress")
		}
	}

	// check the resolution mode of the new endpoint spec. If it's
	// ResolutionModeDNSRoundRobin, then we need to deallocate vips, not
	// allocate them.
	if endpointSpec.Mode == api.ResolutionModeDNSRoundRobin {
		a.ipam.DeallocateVIPs(endpoint)
		endpoint.VirtualIPs = nil
	} else {
		// we don't need to allocate vips if the publish mode is DNSRR
		if err := a.ipam.AllocateVIPs(endpoint, ids); err != nil {
			// if the error is a result of anything other than the fact that we're
			// already allocated, return it
			if !errors.IsErrAlreadyAllocated(err) {
				return err
			}
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

// DeallocateService takes a service and frees its network resources.
func (a *allocator) DeallocateService(service *api.Service) error {
	if service.Endpoint != nil {
		// we can just straight away commit the proposal, no need to wait when
		// deallocating
		prop := a.port.Deallocate(service.Endpoint)
		prop.Commit()
		a.ipam.DeallocateVIPs(service.Endpoint)
	}
	delete(a.services, service.ID)
	return nil
}

// AllocateTask take a task and allocates its network resources. The network
// allocator keeps track of which services are in use, and fills in the task
// with the Endpoint of its corresponding service.
//
// Before calling AllocateTask, the caller must make sure that the service is
// fully allocated. If the service's allocation state is out of data, the task
// wil inherit that out of date state.
//
// AllocateTask can only be called on New tasks, and should only be called
// once. It cannot handle task updates.
//
// If the return value if nil, then the task has been fully allocated.
// Otherwise, the task has not been allocated at all. This method will never
// leave the task in a partially allocated state.
func (a *allocator) AllocateTask(task *api.Task) (rerr error) {
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
	attachmentConfigs := make([]*api.NetworkAttachmentConfig, len(task.Spec.Networks))
	// we're copying here so we can append to this array and make the
	// implementation of allocating attachments simpler. This is a bit
	// expensive, because it's a copy, but there shouldn't be many network
	// attachment configs, and they're just pointers anyway.
	copy(attachmentConfigs, task.Spec.Networks)
	if task.Endpoint != nil && ingressNeeded(task.Endpoint.Ports) {
		if a.ingressID != "" {
			// this is safe, because it won't modify the list on the spec.
			attachmentConfigs = append(attachmentConfigs,
				// we only need to provide the ingress ID as the target in a
				// network attachment config.
				&api.NetworkAttachmentConfig{Target: a.ingressID},
			)
		} else {
			// see the explanation of this error in the analogous code block in
			// AllocateService
			return errors.ErrDependencyNotAllocated("network", "ingress")
		}
	}
	// set up a slice to contain all of the attachments we will create. to make
	// rolling back easier, we'll keep two lists, one for overlay networks and
	// one for node-local networks
	finalAttachments := make([]*api.NetworkAttachment, 0, len(attachmentConfigs))
	localAttachments := []*api.NetworkAttachment{}
	// and set up a defer to roll back attachments. We may have some
	// attachments succeed before one fails. If one does fail, we should
	// deallocate all of the ones that succeeded
	defer func() {
		if rerr != nil {
			for _, attachment := range finalAttachments {
				a.ipam.DeallocateAttachment(attachment)
			}
		}
	}()

	// now go through and allocate all of the attachment
	for _, config := range attachmentConfigs {
		var attachment *api.NetworkAttachment
		// check if the network is node-local. If so, then we do not pass
		// through the IPAM allocator; we just create the attachment here,
		// passing through all of the relevant configuration
		if nw, ok := a.nodeLocalNetworks[config.Target]; ok {
			attachment = &api.NetworkAttachment{
				Network:              nw,
				Aliases:              config.Aliases,
				Addresses:            config.Addresses,
				DriverAttachmentOpts: config.DriverAttachmentOpts,
			}
			localAttachments = append(localAttachments, attachment)
		} else {
			var err error
			attachment, err = a.ipam.AllocateAttachment(config)
			if err != nil {
				return err
			}
			finalAttachments = append(finalAttachments, attachment)
		}
	}

	task.Networks = append(finalAttachments, localAttachments...)
	return nil
}

// DeallocateTask takes a task and frees its network resources.
func (a *allocator) DeallocateTask(task *api.Task) error {
	return a.deallocateAttachments(task.Networks)
}

// AllocateNode allocates the network attachments for a node. The second
// argument, a set of networks, is used to indicate which networks the node
// needs to be attached to. This is necessary because the node's attachments
// are informed by its task allocations, which is a list not available in this
// context.
//
// If this method returns nil, then the node has been fully allocated, and
// should be committed. Otherwise, the node will not be altered.
func (a *allocator) AllocateNode(node *api.Node, requestedNetworks map[string]struct{}) (rerr error) {
	// TODO(dperny): After the initial version, we should remove the code
	// supporting the singular "attachment" field, and require all upgrades
	// past this version to pass through this version in order to reallocate
	// out of that field.
	networks := map[string]struct{}{}

	// copy the networks map so we can safely mutate it
	for nw := range requestedNetworks {
		networks[nw] = struct{}{}
	}

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

	// TODO(dperny): code to support the singular network attachment. remove.
	if node.Attachment != nil && node.Attachment.Network != nil {
		if _, ok := networks[node.Attachment.Network.ID]; ok {
			keep = append(keep, node.Attachment)
			delete(networks, node.Attachment.Network.ID)
			// we can go ahead and nil out the attachment
		} else {
			remove = append(remove, node.Attachment)
		}
	}

	// check here if we're fully allocated on this node already. this is the
	// case if:
	//   1. We are keeping the same number of attachments
	//   2. No networks remain in the networks map (every one is already in keep)
	if len(keep) == len(node.Attachments) && len(networks) == 0 {
		return errors.ErrAlreadyAllocated()
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

	finalAttachments := make([]*api.NetworkAttachment, 0, len(allocate))
	defer func() {
		if rerr != nil {
			for _, attachment := range finalAttachments {
				a.ipam.DeallocateAttachment(attachment)
			}
		}
	}()

	for _, config := range allocate {
		// we don't need to check if networks are node-local, because the
		// caller is only going to pass in overlay networks. If they pass in
		// something else, it will fail in the AllocateAttachment function
		// because the network will not be allocated.
		attachment, err := a.ipam.AllocateAttachment(config)
		if err != nil {
			return err
		}
		finalAttachments = append(finalAttachments, attachment)
	}

	// do NOT return an error if deallocate fails. allocation succeeded, we're
	// fine. we can't safely roll back a deallocation anyway
	for _, attachment := range remove {
		a.ipam.DeallocateAttachment(attachment)
	}

	node.Attachments = append(keep, finalAttachments...)
	// TODO(dperny): code to support the singular network attachment nil out
	// the singular node attachment, so we never have to think of it again.
	node.Attachment = nil
	return nil
}

// DeallocateNode takes a node and frees its associated network resources.
func (a *allocator) DeallocateNode(node *api.Node) error {
	// handle the case of old nodes with the deprecated "attachment" field
	// node.Attachment will probably be nil, but this shouldn't be a problem
	var finalErr error
	if node.Attachment != nil {
		finalErr = a.deallocateAttachments([]*api.NetworkAttachment{node.Attachment})
	}
	if err := a.deallocateAttachments(node.Attachments); err != nil {
		finalErr = err
	}
	return finalErr
}

func (a *allocator) deallocateAttachments(attachments []*api.NetworkAttachment) error {
	var finalErr error
	for _, attachment := range attachments {
		// don't call ipam deallocate on node-local networks
		if _, ok := a.nodeLocalNetworks[attachment.Network.ID]; ok {
			continue
		}
		if err := a.ipam.DeallocateAttachment(attachment); err != nil {
			// if the error is ErrDependencyNotAllocated, then that means the
			// network has already been deallocated. no actually meaningful
			// error has occurred, and nothing needs to be done. otherwise, we
			// will return the last error we received.
			if !errors.IsErrDependencyNotAllocated(err) {
				finalErr = err
			}
		}
	}
	return finalErr
}

// isServiceFullyAllocated takes a service and returns true if its endpoint
// matches its spec and there is no allocation required. It is broken out into
// its own method for ease of testing.
func (a *allocator) isServiceFullyAllocated(service *api.Service) bool {
	// this is kind of tricky... we need to figure out which service
	// endpoints are fully allocated or not here so we can add the fully
	// allocated ones to the services map. note that even if a service
	// isn't fully allocated, we still need to pass it to the Restore
	// methods, because we absolutely must have the entire state, fully
	// allocated or not, before we can pursue new allocations.

	// create a guaranteed non-nil endpoint so we can proceed correctly
	endpoint := &api.Endpoint{Spec: &api.EndpointSpec{}}
	// if the service has a nil endpoint and a non-nil spec, we're not allocated
	if service.Endpoint != nil {
		endpoint = service.Endpoint
		if endpoint.Spec == nil {
			endpoint.Spec = &api.EndpointSpec{}
		}
	}
	spec := &api.EndpointSpec{}
	if service.Spec.Endpoint != nil {
		spec = service.Spec.Endpoint
	}
	// if the mode differs, the service isn't fully allocated
	if endpoint.Spec.Mode != spec.Mode {
		return false
	}
	// if we're using vips, check that we're using the right vips
	if spec.Mode == api.ResolutionModeVirtualIP {
		ingress := ingressNeeded(spec.Ports)
		// networksWithVips is a map of all the network IDs that are present
		// in the VIPs, which tells us if every network attachment has a VIP
		// allocated. this catches a bizarre edge case where a network has two
		// vips.
		networksWithVips := map[string]struct{}{}
		// i'm not totally happy with this part because I think it slightly
		// breaks the separation of concerns, but i think it's more
		// important to guard the ipam package from the details of services
		// and tasks than to guard the network allocator package from the
		// details of IP addresses.
	vipsLoop:
		for _, vip := range endpoint.VirtualIPs {
			// first, if we need ingress, check if this VIP is for ingress.
			// if it is, then it won't be in the spec's networks, but it is
			// supposed to be there, so we can skip looking for it. If
			// ingress ISN'T needed but we find it in the VIPs, then it
			// will fall through this case, pass through the spec's
			// networks loop without continuing, and then return false
			// because it's not supposed to be there.
			if ingress && vip.NetworkID == a.ingressID {
				networksWithVips[vip.NetworkID] = struct{}{}
				continue vipsLoop
			}
			// NOTE(dperny): this does _not_ cover the deprecated
			// service.Spec.Networks field.
			for _, nw := range service.Spec.Task.Networks {
				if nw != nil && nw.Target == vip.NetworkID {
					// if the network is node local, then we are not fully
					// allocated because we have a vip for a network that
					// shouldn't have one
					if _, ok := a.nodeLocalNetworks[nw.Target]; ok {
						return false
					}
					// if we find a target that matches this vip, then
					// we can go to the next VIP and check it
					networksWithVips[vip.NetworkID] = struct{}{}
					continue vipsLoop
				}
			}
			// if we get all the way through the networks and there is
			// nothing matching this VIP, the service isn't fully
			// allocated
			return false
		}

		// now check that every network that should have a vip has one
		for _, attach := range service.Spec.Task.Networks {
			// skip any node-local networks, which should not have attachments
			if _, ok := a.nodeLocalNetworks[attach.Target]; !ok {
				// if we have a network attachment without a corresponding VIP,
				// then we're not fully allocated
				if _, ok := networksWithVips[attach.Target]; !ok {
					return false
				}
			}
		}
	}
	// if we got this far, and the ports are also already allocated,
	// then the service is fully allocated and we can track it in our
	// map.
	if !port.AlreadyAllocated(endpoint, spec) {
		return false
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

// isIngress returns "true" if the provided network is an ingress network
func isIngress(nw *api.Network) bool {
	// ingress networks should have the ingress field set on the spec
	if nw.Spec.Ingress {
		// TODO(dperny): handle partially allocated ingress networks
		return true
	}

	// however, some older networks indicate that they're ingress with
	// labels.
	_, ok := nw.Spec.Annotations.Labels["com.docker.swarm.internal"]
	if ok && nw.Spec.Annotations.Name == "ingress" {
		return true
	}
	return false
}
