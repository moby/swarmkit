package ipam

import (
	"fmt"
	"net"

	"github.com/docker/libnetwork/ipamapi"
	"github.com/docker/libnetwork/netlabel"
	"github.com/docker/libnetwork/types"

	"github.com/docker/swarmkit/api"
	"github.com/docker/swarmkit/manager/allocator/network/errors"
)

// DrvRegistry is an interface defining the methods we need to retrieve IPAM
// drivers. It is a strict subset of the methods defined on a
// github.com/docker/libnetwork/drvregistry.DrvRegistry object, and exists to
// make testing the IPAM allocator simpler.
type DrvRegistry interface {
	IPAM(name string) (ipamapi.Ipam, *ipamapi.Capability)
}

// network is a private type used to hold the internal state of a network in
// the IP Allocator
type network struct {
	// nw is a local cache of the network object found in the store
	nw *api.Network

	// pools is used to save the internal poolIDs needed when releasing a pool.
	// It it maps the subnet of one of a network's IPAMConfigs (specifically,
	// nw.IPAM.Configs[i].Subnet) to the poolID from which that config
	// allocates addresses.
	pools map[string]string

	// when allocating addresses, we must know to which pool those addresses
	// belong, so they can be properly freed from that same pool. This maps
	// an IP address that has been allocated for this network to the poolID
	// it was allocated from
	endpoints map[string]string
}

// Allocator is an interface that represents the IP address allocator. It
// exists mainly for testing purposes, so that the allocator can be more easily
// mocked out.
type Allocator interface {
	Restore([]*api.Network, []*api.Endpoint, []*api.NetworkAttachment) error
	AllocateNetwork(*api.Network) error
	DeallocateNetwork(*api.Network)
	AllocateVIPs(*api.Endpoint, map[string]struct{}) error
	DeallocateVIPs(*api.Endpoint)
	AllocateAttachment(*api.NetworkAttachmentConfig) (*api.NetworkAttachment, error)
	// DeallocateAttachment is the only Deallocate method that returns an
	// error. It is desirable for them all to do so, but the error handling
	// flow for deallocation has not been fully designed yet, and its benefits
	// are comparatively marginal, so in the interest of time, the other
	// methods do not have error returns implemented yet.
	DeallocateAttachment(*api.NetworkAttachment) error
}

// allocator is an allocator for IP addresses and IPAM pools. It handles all
// allocation and deallocation of IP addresses, both for VIPs and endpoints, in
// addition to handling IPAM pools for networks.
type allocator struct {
	// networks maps network IDs to the locally stored network object
	networks map[string]*network

	// drvRegistry is the driver registry, from which we can get active network
	// drivers
	drvRegistry DrvRegistry
}

// NewAllocator takes a drvRegistry and creates a new IP allocator
func NewAllocator(reg DrvRegistry) Allocator {
	return &allocator{
		networks:    make(map[string]*network),
		drvRegistry: reg,
	}
}

// Restore restores the state of the provided networks to the Allocator. It can
// return errors if the initialization of its dependencies fails.
func (a *allocator) Restore(networks []*api.Network, endpoints []*api.Endpoint, attachments []*api.NetworkAttachment) error {
	// Initialize the state of the networks. this gets the initial IPAM pools.
	for _, nw := range networks {
		// if the network has no IPAM field, it has no state, and there is
		// nothing to do
		if nw.IPAM == nil ||
			// if the network has no IPAM driver, it has no IPAM state, and there
			// is nothing to do.
			nw.IPAM.Driver == nil ||
			// if the network has no ipam configs, then it has no state, and there
			// is nothing to do
			len(nw.IPAM.Configs) == 0 {
			continue
		}
		local := &network{
			nw:        nw,
			pools:     make(map[string]string),
			endpoints: make(map[string]string),
		}
		// TODO(dperny): is it possible for a network before this change to
		// have an empty ipamName but be allocated?
		ipamName := nw.IPAM.Driver.Name
		ipamOpts := nw.IPAM.Driver.Options
		if ipamOpts == nil {
			ipamOpts = map[string]string{}
		}
		// if we have an ipam driver name and we have IPAM configs, but for
		// some reason we don't have IPAM options, just fill that field in.
		// shouldn't do any harm. i doubt we'll ever hit this in prod
		local.nw.IPAM.Driver.Options = ipamOpts
		// IPAM returns the Ipam object and also its capabilities. We don't
		// use the capabilities or care about them right now, so just ignore
		// that part of the return value
		ipam, _ := a.drvRegistry.IPAM(ipamName)
		if ipam == nil {
			return errors.ErrBadState("ipam driver %q cannot be found", ipamName)
		}
		_, addressSpace, err := ipam.GetDefaultAddressSpaces()
		// the only errors here result from having an invalid ipam driver
		if err != nil {
			return errors.ErrInternal("ipam %v for network %v returned an error when requesting the default address space: %v", ipamName, nw.ID, err)
		}

		// now initialize the IPAM pools. IPAM pools are the set of addresses
		// available for a specific network. There is one IPAM config for every
		// pool. In order for this restore operation to be consistent, a
		// network must have no ipam configs if it hasn't been allocated
		for _, config := range local.nw.IPAM.Configs {
			// the last param of RequestPool is "v6", meaning IPv6, which we
			// don't support, hence passing "false"
			poolID, poolIP, _, err := ipam.RequestPool(addressSpace, config.Subnet, config.Range, ipamOpts, false)
			if err != nil {
				// typically, if there was an error in requesting a pool, we
				// would release the pools we've already allocated. However, if
				// there is an error at this stage, it means the whole object
				// store is in a bad state, so we don't do that, we just
				// abandon everything.
				return errors.ErrBadState(
					"error reserving ipam pool for network %v: %v",
					nw.ID, err,
				)
			}
			// each IPAM pool we use has a separate ID referring to it. We also
			// keep a map of each IP address we have allocated for this network
			// and what pool it belongs to, so we can deallocate an address
			// from the correct pool
			local.pools[poolIP.String()] = poolID
			// now we just need to reserve the gateway address for this pool.
			// set the IPAM request address type to "Gateway" to tell the IPAM
			// driver that's the kind of address we're requesting

			// NOTE(dperny): i'm not sure if there are other address types that
			// can be requested, or if this field can be set to any other
			// values.  however, to be safe, if there is a value already in the
			// ipamOpts, save it so we put this map back how it was before
			prevReqAddrType, hasPrevReqAddrType := ipamOpts[ipamapi.RequestAddressType]
			ipamOpts[ipamapi.RequestAddressType] = netlabel.Gateway
			// and now, if we have a Gateway address for this network, we need
			// to allocate it. The inverse condition, a network with no gatewa,
			// should never happen; a network with a valid IPAM config but an
			// empty gateway is a recipe for disaster. This is just included
			// for completeness if an older version of the allocator has
			// incorrect state.
			if config.Gateway != "" {
				_, _, err := ipam.RequestAddress(poolID, net.ParseIP(config.Gateway), ipamOpts)
				if err != nil {
					return errors.ErrBadState(
						"error requesting already assigned gateway address %v: %v",
						err,
					)
				}
				// NOTE(dperny): this check was originally here:
				// if gwIP.IP.String() != config.Gateway {
				//   // if we get an IP address from this that isn't the one we
				//   // requested, that's Very Bad.
				//   return ErrBadState{
				//     local.nw.ID,
				//     fmt.Sprintf("got back gateway ip %v, but requested ip %v", gwIP, config.Gateway),
				//   }
				// }
				// it has been removed because we don't need it. this is a
				// check that IPAM is behaving correctly. We don't need to
				// check that IPAM is behaving correctly. Doing so makes this
				// code less clear and harder to test. This has been left in
				// so that this bolt of wisdom is shared with future
				// contributors. A similar check was found in restoreAddress
				// in the analogous location.

				// most addresses need to be added to the map of
				// address -> poolid, but we don't need to do this with the
				// gateway address because it's stored in the ipam config with
				// the subnet, which is the key for the pools map.
			}
			// if there was previously a RequestAddressType set, then restore
			// this map entry to the way it was before. Otherwise, delete the
			// map entry.
			if hasPrevReqAddrType {
				ipamOpts[ipamapi.RequestAddressType] = prevReqAddrType
			} else {
				delete(ipamOpts, ipamapi.RequestAddressType)
			}
			// finally, add the network to the list of networks we're keeping
			// track of.
			a.networks[local.nw.ID] = local
		}
	}

	// now restore the VIPs and attachment addresses

	// first the VIPs
	for _, endpoint := range endpoints {
		for _, vip := range endpoint.VirtualIPs {
			// there shouldn't be nil vips but lord knows what kind of black
			// magic goes on in other parts of the code or in the old
			// allocator, and it doesn't really cost anything
			// also, check that the VIP address is allocated, so we don't try
			// to allocate a new VIP. that's the most common class of error in
			// the old allocator
			if vip != nil {
				if err := a.restoreAddress(vip.NetworkID, vip.Addr); err != nil {
					// all errors returned from restoreAddress are ultimately
					// ErrBadState, because we should always retore correctly
					return errors.ErrBadState("error restoring vip %v: %v", vip.Addr, err)
				}
			}
		}
	}

	// now the attachments
	for _, attachment := range attachments {
		// nil checking for the same reaason as VIPs. why is everything a
		// pointer
		if attachment != nil && attachment.Network != nil {
			nwid := attachment.Network.ID
			for _, addr := range attachment.Addresses {
				// using restoreAddress here ensures that we can't accidentally
				// perform allocation.
				if err := a.restoreAddress(nwid, addr); err != nil {
					return errors.ErrBadState("error restoring address %v: %v", addr, err)
				}
			}
		}
	}

	// finally, everything is reallocated and we're ready to go and allocate
	// new things.
	return nil
}

// restoreAddress is the common functionality needed to mark a given address in
// use for the given network ID. if the address given is accidentally empty,
// we'll return nil as there is nothing to restore but no error has occurred
func (a *allocator) restoreAddress(nwid string, address string) error {
	// this check on address is shared by restoring VIPs and attachments.
	if address == "" {
		return nil
	}
	// first, get the local network state and IPAM driver
	local, ok := a.networks[nwid]
	if !ok {
		return errors.ErrDependencyNotAllocated("network", nwid)
	}
	ipam, _ := a.drvRegistry.IPAM(local.nw.IPAM.Driver.Name)
	if ipam == nil {
		return errors.ErrInvalidSpec(
			"ipam driver %v cannot be found", local.nw.IPAM.Driver.Name,
		)
	}
	ipamOpts := local.nw.IPAM.Driver.Options
	// NOTE(dperny): this code, where we try parsing as CIDR and
	// then as a regular IP, is from the old allocator. I do not
	// know why this is done this way
	addr, _, err := net.ParseCIDR(address)
	if err != nil {
		addr = net.ParseIP(address)
		if addr == nil {
			return errors.ErrInvalidSpec("address %v is not valid", address)
		}
	}
	// we don't know which pool this address belongs to, so go through each
	// pool in this network and try to request this address.
	//
	// NOTE(dperny): this code is couched in 2 assumptions:
	//   - IPAM pools can't overlap
	//   - IPAM address requests will prefer "out of range" to "no available
	//	   IPs"
	// The first one I'm rather sure of, but the second i'm not... I don't know
	// what the response would be if the pool had no remaining addresses but
	// the requested address was out of range anyway
	for _, poolID := range local.pools {
		ip, _, err := ipam.RequestAddress(poolID, addr, ipamOpts)
		if err == ipamapi.ErrIPOutOfRange {
			continue
		}
		if err != nil {
			return errors.ErrBadState(
				"error restoring network %v when requesting address %v: %v",
				local.nw.ID,
				addr.String(),
				err,
			)
		}
		// if we get this far, the address belongs to this pool. add to the
		// endpoints map for deallocation later and return nil, for no error
		local.endpoints[ip.String()] = poolID
		return nil
	}

	// if we get all the way through this loop, without jumping to
	// the next iteration of the addresses loop, then we're in a
	// weird situation where the address is out of range for
	// _every_ pool on the network.
	return errors.ErrBadState(
		"error restoring network %v when requesting address %v: %v",
		local.nw.ID,
		addr.String(),
		"address is out of range for all pools",
	)
}

// AllocateNetwork allocates the IPAM pools for the given network. The network
// must not be nil, and must not use a node-local driver.
//
// We can't use a node-local driver, because we don't have access to the
// network's driver information. We only have acccess to the IPAM driver.
//
// If successful, this method will mutate the provided network, but will not
// otherwise.
func (a *allocator) AllocateNetwork(n *api.Network) (rerr error) {
	// check if this network is already being managed and return an error if it
	// is. networks are immutable and cannot be updated.
	if _, ok := a.networks[n.ID]; ok {
		return errors.ErrAlreadyAllocated()
	}

	// Now get the IPAM driver and options, either the defaults or the user's
	// specified options.
	ipamName := ipamapi.DefaultIPAM
	if n.Spec.IPAM != nil && n.Spec.IPAM.Driver != nil && n.Spec.IPAM.Driver.Name != "" {
		ipamName = n.Spec.IPAM.Driver.Name
	}
	ipam, _ := a.drvRegistry.IPAM(ipamName)
	if ipam == nil {
		return errors.ErrInvalidSpec("ipam driver %v cannot be found", ipamName)
	}

	// make sure here that ipamOpts is not nil so we don't need to nil check it
	// everywhere later
	ipamOpts := map[string]string{}
	if n.Spec.IPAM != nil && n.Spec.IPAM.Driver != nil && n.Spec.IPAM.Driver.Options != nil {
		ipamOpts = n.Spec.IPAM.Driver.Options
		// copy the spec to a new map
		for k, v := range n.Spec.IPAM.Driver.Options {
			ipamOpts[k] = v
		}
	}
	// if the IPAM serial allocation option has not been explicitly set one
	// way or another, set it here. this (roughly speaking) encourages IPAM to
	// prefer allocating addresses that haven't been recently freed, mitigating
	// (but not eliminating) some issues with objects being deleted before
	// they've fully finished terminating.
	//
	// It is the expected behavior, tested for in the docker cli integration
	// tests, that this option is set on the IPAM options, so after we set it
	// we need to write it out to the driver.ipamOpts field
	if _, ok := ipamOpts[ipamapi.AllocSerialPrefix]; !ok {
		ipamOpts[ipamapi.AllocSerialPrefix] = "true"
	}

	_, addressSpace, err := ipam.GetDefaultAddressSpaces()
	if err != nil {
		return errors.ErrInternal("ipam %v for network %v returned an error when requesting the default address space: %v", ipamName, n.ID, err)
	}

	// now create the local network state object
	local := &network{
		nw:        n,
		pools:     map[string]string{},
		endpoints: map[string]string{},
	}
	var ipamConfigs []*api.IPAMConfig
	if n.Spec.IPAM != nil && len(n.Spec.IPAM.Configs) != 0 {
		// if the user has specified any IPAM configs, we'll use those
		ipamConfigs = n.Spec.IPAM.Configs
	} else {
		// otherwise, we'll create a single default IPAM config.
		ipamConfigs = []*api.IPAMConfig{
			{
				Family: api.IPAMConfig_IPV4,
			},
		}
	}
	// make the slice to hold final IPAM configs
	finalConfigs := make([]*api.IPAMConfig, 0, len(ipamConfigs))
	// before we start allocating from ipam, set up this defer to roll back
	// allocation in the case that allocation of any particular pool fails
	defer func() {
		if rerr != nil {
			for _, config := range finalConfigs {
				// only free addresses that were actually allocated
				if ip := net.ParseIP(config.Gateway); ip != nil {
					if err := ipam.ReleaseAddress(local.pools[config.Subnet], ip); err != nil {
						rerr = errors.ErrInternal(
							"an error occurred when rolling back a partially successful allocation. originally: %v, now: %v",
							rerr,
							err,
						)
						return
					}
				}
			}
			for _, pool := range local.pools {
				if err := ipam.ReleasePool(pool); err != nil {
					rerr = errors.ErrInternal(
						"an error occurred when rolling back a partially successful allocation. originally: %v, now: %v",
						rerr,
						err,
					)
					return
				}
			}
		}
	}()

	// now go through all of the IPAM configs and allocate them. in the
	// process, copy those configs to the object's configs. we do copies in
	// order to avoid inadvertently modifying the spec.
	//
	// NOTE(dperny): be careful with this! if this loop doesn't run (because
	// there were no items in ipamConfigs) then this will fail silently!
	for _, specConfig := range ipamConfigs {
		config := specConfig.Copy()
		// the last parameter of this is "v6 bool", but we don't support ipv6
		// so we just pass "false"
		poolID, poolIP, meta, err := ipam.RequestPool(addressSpace, config.Subnet, config.Range, ipamOpts, false)
		if err != nil {
			if _, ok := err.(types.BadRequestError); ok {
				return errors.ErrInvalidSpec("invalid network spec: %v", err)
			}
			if err == ipamapi.ErrPoolOverlap {
				errors.ErrResourceInUse("pool",
					fmt.Sprintf("with subnet %v and range %v", config.Subnet, config.Range),
				)
			}
			if _, ok := err.(types.NoServiceError); ok {
				return errors.ErrResourceExhausted("pools", err.Error())
			}
			return errors.ErrInternal("error requesting pool with ipam %v: %v", ipamName, err)
		}
		local.pools[poolIP.String()] = poolID
		// The IPAM contract allows the IPAM driver to autonomously provide a
		// network gateway in response to the pool request.  But if the network
		// spec contains a gateway, we will allocate it irrespective of whether
		// the ipam driver returned one already.  If none of the above is true,
		// we need to allocate one now, and let the driver know this request is
		// for the network gateway.
		var (
			gwIP *net.IPNet
			ip   net.IP
		)

		if gws, ok := meta[netlabel.Gateway]; ok {
			if ip, gwIP, err = net.ParseCIDR(gws); err != nil {
				return errors.ErrInternal(
					"can't parse gateway address (%v) returned by the ipam driver: %v",
					gws, err,
				)
			}
			gwIP.IP = ip
		}
		// add the option indicating that we're gonna request a gateway, and
		// remove it before we exit this function
		// NOTE(dperny): I don't think there are alternate values of
		// RequestAddressType, but just in case, we should save whatever
		// previous value there may have been
		prevReqAddrType, hasPrevReqAddrType := ipamOpts[ipamapi.RequestAddressType]
		ipamOpts[ipamapi.RequestAddressType] = netlabel.Gateway
		defer func() {
			// if there was a value, set it back to that value. otherwise, just
			// delete from the map
			if hasPrevReqAddrType {
				ipamOpts[ipamapi.RequestAddressType] = prevReqAddrType
			} else {
				delete(ipamOpts, ipamapi.RequestAddressType)
			}
		}()
		if config.Gateway != "" || gwIP == nil {
			gwIP, _, err = ipam.RequestAddress(poolID, net.ParseIP(config.Gateway), ipamOpts)
			if err != nil {
				if err == ipamapi.ErrIPAlreadyAllocated {
					return errors.ErrResourceInUse("ip", config.Gateway)
				}
				return errors.ErrInternal(
					"error requesting gateway ip %v: %v", config.Gateway, err,
				)
			}
		}
		if config.Subnet == "" {
			config.Subnet = poolIP.String()
		}
		if config.Gateway == "" {
			config.Gateway = gwIP.IP.String()
		}
		finalConfigs = append(finalConfigs, config)
	}

	// now that everythign has succeeded, add the fields to the network object
	n.IPAM = &api.IPAMOptions{
		Driver: &api.Driver{
			Name:    ipamName,
			Options: ipamOpts,
		},
	}
	n.IPAM.Configs = finalConfigs

	// finally, add this network to the set of allocated networks.
	a.networks[n.ID] = local
	return nil
}

// DeallocateNetwork takes a network that has been allocated, and releases all
// of the IPAM resource associated with it. It then removes the IPAM config
// from the object, returning with it in an unallocated state.
func (a *allocator) DeallocateNetwork(network *api.Network) {
	local, ok := a.networks[network.ID]
	if !ok {
		// if the network was never allocated, nothing to do
		return
	}

	// we know, because we allocated this network, that its IPAM field is fully
	// allocated, and we can use it without nil checking
	ipam, _ := a.drvRegistry.IPAM(local.nw.IPAM.Driver.Name)

	// TODO(dperny): if the network has outstanding dependencies, deallocating
	// will cause us to enter a bad state.

	for _, config := range local.nw.IPAM.Configs {
		// these things can return errors, but we literally can't do anything
		// about it if they do, so just ignore it.
		ipam.ReleaseAddress(local.pools[config.Subnet], net.ParseIP(config.Gateway))
		ipam.ReleasePool(local.pools[config.Subnet])
	}
	// remove the IPAM config. this may be useful for "rolling back" a network
	// allocation.
	// NOTE(dperny): doing this can cause a data race. i believe it may be
	// possible that the same network object is shared across event receivers.
	// network.IPAM = nil
	delete(a.networks, network.ID)
}

// AllocateVIPs allocates the VIPs for the provided endpoint and network ids.
// The endpoint is assumed to be in resolution mode
// api.ResolutionModeVirtualIP, which should be verified before calling. This
// is the case because the endpoint object may reflect an older spec, instead
// of the current one.
//
// If successful, this method will mutate the provided endpoint, but will not
// otherwise do so.
func (a *allocator) AllocateVIPs(endpoint *api.Endpoint, networkIDs map[string]struct{}) (rerr error) {
	// first, go through and check that every network we want a VIP for is
	// allocated. if not, return an error. We can't allocate VIPs until the
	// network is allocated
	for nwid := range networkIDs {
		if _, ok := a.networks[nwid]; !ok {
			return errors.ErrDependencyNotAllocated("network", nwid)
		}
	}

	// now compute the changes
	allocate := []string{}
	// keep is the set of all virtual IPs we'll retain between the previous and
	// current spec. we make it with a capacity the same as the current vips
	// because typically, the vips in use won't change much, and we can avoid
	// allocation by using this as a guess
	keep := make([]*api.Endpoint_VirtualIP, 0, len(endpoint.VirtualIPs))
	deallocate := []*api.Endpoint_VirtualIP{}
	// nwidsInVips is a set of all of network IDs with a VIP currently
	// allocated, used to verify which vips we already have allocated.
	nwidsInVips := make(map[string]struct{}, len(endpoint.VirtualIPs))

	// go through all of the VIPs we  have, and sort them into VIPs we're
	// keeping and vips we're removing. in addition, make note of which
	// networks have a VIP allocated already, so we can quickly figure out
	// which networks we need to allocate for.
	//
	// NOTE(dperny): another possible optimization may be to copy the
	// networkIDs map, and then delete each network ID found in the endpoint
	// already from the map, leaving us after with a map containing only the
	// network IDs we need to newly allocate.
	for _, vip := range endpoint.VirtualIPs {
		if _, ok := networkIDs[vip.NetworkID]; ok {
			keep = append(keep, vip)
		} else {
			deallocate = append(deallocate, vip)
		}
		nwidsInVips[vip.NetworkID] = struct{}{}
	}

	// now go through all the networks we desire to have allocated. If any of
	// those networks does not already have an VIP allocated on the endpoint,
	// add it to the list of networks we're allocating.
	for nwid := range networkIDs {
		if _, ok := nwidsInVips[nwid]; !ok {
			allocate = append(allocate, nwid)
		}
	}

	// create a new slice to hold the vips we're allocating now
	newVips := make([]*api.Endpoint_VirtualIP, 0, len(allocate))

	// set up a deferred function to roll back any allocation that has
	// succeeded if later allocation fails.
	defer func() {
		if rerr != nil {
			a.deallocateVIPs(newVips)
		}
	}()

allocateLoop:
	for _, nwid := range allocate {
		// we already verified that every one of the requested networks
		// existed, so again, no need to check ok
		local := a.networks[nwid]
		// we don't need to nil check any of the intermediate fields. we made
		// them we we know they're filled in. likewise, we don't need to check
		// that the ipam exists; if it existed when we allocated or restored
		// the network, it should exist now.
		ipam, _ := a.drvRegistry.IPAM(local.nw.IPAM.Driver.Name)
		opts := local.nw.IPAM.Driver.Options
		// the network may have several pools of IP addresses, and some of them
		// may be full, so we need to try allocation on every pool available
		// for the network until we find a pool that succeeds.
		for _, poolID := range local.pools {
			// passing nil for the second args indicates that we don't have any
			// particular address in mind to allocate
			ip, _, err := ipam.RequestAddress(poolID, nil, opts)
			// if there's no error, we have a valid address and we're done
			if err == nil {
				// add it to the endpoints map so we can figure out what pool
				// it belongs to when we deallocate
				local.endpoints[ip.String()] = poolID
				// add a new VIP object to our slice
				newVips = append(newVips, &api.Endpoint_VirtualIP{
					NetworkID: nwid,
					// ip.String() will return an IP in CIDR notation. that's
					// intended, current behavior is for vips to be CIDR
					// notation.
					Addr: ip.String(),
				})
				// continue allocate loop, to skip the error handling that
				// occurs when ever pool has been exhausted
				continue allocateLoop
			}
			// if we get ErrNoAvailableIPs, it means this pool is already full,
			// and we'll go on to the next one. if we get any other error,
			// that's fatal, so we'll return it and clean up
			if err != ipamapi.ErrNoAvailableIPs {
				return errors.ErrInternal("error allocating new IP address: %v", err)
			}
		}
		// if we get here, that means we've tried every pool on the network,
		// and all of them are exhausted
		return errors.ErrResourceExhausted("ip address", "no IPs remaining for network %v", nwid)
	}

	// Elsewhere in the code we might deallocate first, and then allocate, that
	// way if we're approaching resource exhaustion we can reuse some of our
	// own freed resources. However, because each VIP belongs to a different
	// network, and each network in turn has non-overlapping subnets, there is
	// no chance of IPs we're releasing to be reused in the allocation of new
	// VIPs. So, instead, we deallocate last, so that if any allocation fails,
	// we only have to roll back incomplete allocation, not re-allocate a
	// release. We don't have to worry about re-allocating if deallocate fails,
	// because if deallocation fails we are in a world of hurt.
	a.deallocateVIPs(deallocate)

	// now we've allocated every new vip. Add them all to our held over VIPs,
	// and return nil
	endpoint.VirtualIPs = append(keep, newVips...)
	return nil
}

// DeallocateVIPs releases all of the VIPs in the endpoint.
func (a *allocator) DeallocateVIPs(endpoint *api.Endpoint) {
	a.deallocateVIPs(endpoint.VirtualIPs)
}

func (a *allocator) deallocateVIPs(deallocate []*api.Endpoint_VirtualIP) {
	for _, vip := range deallocate {
		// we know the network is allocated, because we allocated it, and
		// because the higher levels won't allow the deletion of a network
		// which still has resources attached, so no need to check ok
		local := a.networks[vip.NetworkID]
		// we don't need to check that the IPAM driver is non-nil because the
		// network being successfully allocated indicates that it is not. If it
		// is nil, we should probably crash the program anyway cause that's not
		// right
		ipam, _ := a.drvRegistry.IPAM(local.nw.IPAM.Driver.Name)

		// we don't need to check err, because we set this value to begin with.
		// if we inherited some bogus value from an old iteration of the
		// allocator, we would have errored out on restore anyway
		ip, _, _ := net.ParseCIDR(vip.Addr)
		poolID := local.endpoints[vip.Addr]
		// remove the address from the endpoints map, because we've deallocated
		// it.
		delete(local.endpoints, vip.Addr)
		// ReleaseAddress can return an error...  but look, how on earth do we
		// get a dang error RELEASING an address?  that doesn't even make SENSE
		// to me. i'm ignoring it, just let the program crash if that happens.
		// I don't care what IPAM does after we call release. worst case, we
		// get back to a consistent state on the next leadership change
		ipam.ReleaseAddress(poolID, ip)
	}
}

// AllocateAttachment allocates a single network attachment belonging to a task
// or not. AllocateAttachment takes a NetworkAttachmentConfig and returns a
// NetworkAttachment (and an error). This means it does not mutate its
// argument.
func (a *allocator) AllocateAttachment(config *api.NetworkAttachmentConfig) (attach *api.NetworkAttachment, rerr error) {
	// first, get the network
	// this ONLY WORKS because a network cannot be updated. if a network can be
	// updated, we'd have a lot more headaches.
	local, ok := a.networks[config.Target]
	if !ok {
		return nil, errors.ErrDependencyNotAllocated("network", config.Target)
	}

	attachment := &api.NetworkAttachment{
		Network:              local.nw,
		Aliases:              config.Aliases,
		DriverAttachmentOpts: config.DriverAttachmentOpts,
		Addresses:            make([]string, 0, len(config.Addresses)),
	}

	// now get the IPAM driver and options
	ipam, _ := a.drvRegistry.IPAM(local.nw.IPAM.Driver.Name)
	ipamOpts := local.nw.IPAM.Driver.Options

	// there are two cases. the most common case is that no addresses are
	// specified. This is the case for most tasks. The second case is when some
	// addresses are specified in the config. This is only the case for
	// attachments belonging to a NetworkAttachment tasks, which are used to
	// connect a regular docker container to an attachable networks.
	if len(config.Addresses) == 0 {
		// we need one address for the attachment on the network. the network
		// may have many disjoint IP address pools, and some of them may be
		// exhausted of IP addresses. So, we have to try allocating on each
		// pool belonging to the network until we get one that succeeds
		for _, poolID := range local.pools {
			// passing nil as the 2nd argument tells the IPAM driver that we're
			// requesting no particular address, and to give us whatever is
			// available.
			ip, _, err := ipam.RequestAddress(poolID, nil, ipamOpts)
			if err == nil {
				// keep track of which pool this address belongs to, for
				// deallocation
				local.endpoints[ip.String()] = poolID
				attachment.Addresses = append(attachment.Addresses, ip.String())
				return attachment, nil
			}
			// if we get ErrNoAvailableIPs, that means we should try the next
			// pool. any other error is fatal.
			if err != ipamapi.ErrNoAvailableIPs {
				return nil, errors.ErrInternal("error requesting address from ipam: %v", err)
			}
		}
		// if we get all the way through the loop of pools without returning
		// either an attachment or an error, that means the address space for
		// the network is exhausted
		return nil, errors.ErrResourceExhausted("ip addresses", "address space for network %v is exhausted", local.nw.ID)
	}
	// otherwise, if there are addresses specified, this is a NetworkAttachment
	// task, and we need to allocate those specific addresses

	// if the allocation of any of these addresses fails, they all need to be
	// released
	defer func() {
		if rerr != nil {
			for _, addr := range attach.Addresses {
				poolID := local.endpoints[addr]
				ip := net.ParseIP(addr)
				ipam.ReleaseAddress(poolID, ip)
				delete(local.endpoints, addr)
			}
		}
	}()
	// NOTE(dperny): this behavior is different, I think, from the old
	// allocator, but something is... wrong, like, really strangely wrong, with
	// the old allocator code, and I think it was always broken.
addressesLoop:
	for _, address := range config.Addresses {
		requestIP, _, err := net.ParseCIDR(address)
		if err != nil {
			requestIP = net.ParseIP(address)
			if requestIP == nil {
				return nil, errors.ErrInvalidSpec("address %v is not valid", address)
			}
		}
		// now that we've got the IP address in a usable form, try
		// reserving it. we will need to try for each pool
		for _, poolID := range local.pools {
			var ip *net.IPNet
			ip, _, err := ipam.RequestAddress(poolID, requestIP, ipamOpts)
			if err == nil {
				// we only want 1 address per task. so, once we've found a
				// valid address, break out of the addresses loop, and go
				// to the next attachment.
				local.endpoints[ip.String()] = poolID
				attachment.Addresses = append(attachment.Addresses, ip.String())
				// go to the next address
				continue addressesLoop
			}
			// if we get ErrIPOutOfRange it means this pool might not be the
			// right pool for this address. in that case, try the next pool
			if err == ipamapi.ErrIPOutOfRange {
				continue
			}
			if err == ipamapi.ErrIPAlreadyAllocated {
				return nil, errors.ErrResourceInUse("ip", requestIP.String())
			}
			if err != nil {
				// any other error is internal, not a problem with the spec.
				return nil, errors.ErrInternal("error requesting address %v: %v", address, err)
			}
		}
		// if we get here, it's because the address belongs to none of the
		// pools for this network.
		return nil, errors.ErrInvalidSpec(
			"address %v does not fall in the address space for network %v",
			address, local.nw.ID,
		)
	}
	return attachment, nil
}

// DeallocateAttachment deallocates a single attachment belonging to a task or
// node
func (a *allocator) DeallocateAttachment(attachment *api.NetworkAttachment) error {
	// get the local network and IPAM driver
	local, ok := a.networks[attachment.Network.ID]
	// the only case where we actually try to deallocate an attachment for
	// which the network is not allocated should be node attachments.
	if !ok {
		return errors.ErrDependencyNotAllocated("network", attachment.Network.ID)
	}
	ipam, _ := a.drvRegistry.IPAM(local.nw.IPAM.Driver.Name)

	// if we get an error, we are going to continue through the function, but
	// we will return the last error we receive. this could mean we drop some
	// errors, but it's very likely that this will only ever de-allocate 1
	// address at a time, and even if there are more than 1 the error will
	// probably be the same each time
	var finalErr error
	for _, address := range attachment.Addresses {
		poolID := local.endpoints[address]
		delete(local.endpoints, address)

		// both of these things can error, but we can't do anything about
		// it if they do, so we ignore the errors.
		ip, _, _ := net.ParseCIDR(address)
		// if we get an error
		if err := ipam.ReleaseAddress(poolID, ip); err != nil {
			finalErr = errors.ErrInternal(
				"error while releasing attachment address %v: %v",
				address, err,
			)
		}
	}
	return finalErr
}
