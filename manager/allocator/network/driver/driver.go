package driver

import (
	"net"

	"github.com/docker/docker/pkg/plugingetter"
	"github.com/docker/libnetwork/datastore"
	"github.com/docker/libnetwork/driverapi"

	"github.com/docker/swarmkit/api"
	"github.com/docker/swarmkit/manager/allocator/network/errors"
)

const (
	// DefaultDriver defines the name of the driver to be used by
	// default if a network without any driver name specified is
	// created.
	DefaultDriver = "overlay"
)

// DrvRegistry is an interface implementing the subset of methods from
// driverapi.DrvRegistry that we actually care about. It exists to make testing
// this Allocator easier, by breaking most of the dependency with the
// complexity of driverapi.DrvRegistry
//
// driverapi.DrvRegistry  is a tool that manages internal and external drivers.
// 99% of its use to us is the fact that it keeps track of available drivers,
// and that's it. The remaining 1% is the "remote" driver. That particular kind
// of driver has an Init function that registers a handler with a PluginGetter,
// which in turn contains a callback to the driverapi.DrvRegistry, which, as
// far as I can tell, causes newly added plugins to be available in the
// driverapi.DrvRegistry. It does a bunch of other stuff too that we don't care
// about or use.
type DrvRegistry interface {
	GetPluginGetter() plugingetter.PluginGetter
	Driver(name string) (driverapi.Driver, *driverapi.Capability)
}

// Allocator is the interface for a network driver allocator. The purpose of
// this allocator is to handle the allocate of driver state for networks. The
// Allocator owns the DriverState field on the api.Network object
type Allocator interface {
	Restore([]*api.Network) error
	Allocate(*api.Network) error
	Deallocate(*api.Network) error
	IsNetworkNodeLocal(n *api.Network) (bool, error)
}

type allocator struct {
	drvRegistry DrvRegistry
}

// NewAllocator creates and returns a new driver allocator
func NewAllocator(drvRegistry DrvRegistry) Allocator {
	return &allocator{
		drvRegistry: drvRegistry,
	}
}

// Restore takes a list of networks, and restores the driver state for all of
// the networks that are already allocated.
func (a *allocator) Restore(networks []*api.Network) error {
	for _, n := range networks {
		// networks which are already allocated will have a non-nil driver
		// state.
		if n.DriverState != nil {
			driver, caps, err := a.getDriver(n.DriverState.Name)
			if err != nil {
				return err
			}
			// if this is a local-scoped network, we don't need to restore any
			// driver state, and we can go to the next network
			if caps.DataScope == datastore.LocalScope {
				continue
			}
			// now we restore the driver state
			// get the driver options.
			options := n.DriverState.Options

			ipamData, err := getIpamData(n)
			if err != nil {
				return err
			}
			_, err = driver.NetworkAllocate(n.ID, options, ipamData, nil)
			if err != nil {
				// again, this error case should not occur, because we should
				// not fail when we previously succeeded
				// using ErrInternal instead of ErrBadState because the state
				// is probably valid, but something else has gone wrong
				return errors.ErrInternal("driver.NetworkAllocate call failed: %v", err)
			}
		}
	}
	return nil
}

// Allocate takes a network and allocates it with the network driver, filling
// in the n.DriverState field before returning
func (a *allocator) Allocate(n *api.Network) error {
	// first, get the network driver
	// use the defaul driver, or, if the driver is specified, use the driver
	// provided by the user in the spec
	driverName := DefaultDriver
	if n.Spec.DriverConfig != nil && n.Spec.DriverConfig.Name != "" {
		driverName = n.Spec.DriverConfig.Name
	}
	// the use of getDriver here is kind of redundant, because we would expect
	// the caller to call IsNetworkNodeLocal first, which will do the same
	// retry dance, but we'll leave this in for safety's sake.
	driver, caps, err := a.getDriver(driverName)
	if err != nil {
		return err
	}
	// No swarm-level allocation can be provided by the network driver for
	// node-local networks. Only thing needed is populating the driver's name
	// in the driver's state.
	if caps.DataScope == datastore.LocalScope {
		n.DriverState = &api.Driver{
			Name: driverName,
		}
		// In order to support backward compatibility with older daemon
		// versions which assumes the network attachment to contains
		// non nil IPAM attribute, passing an empty object
		n.IPAM = &api.IPAMOptions{Driver: &api.Driver{}}
		return nil
	}

	var options map[string]string
	// NOTE(dperny): if we supported network updates, we would have to
	// reconcile the driver state options in the spec with those in the object.
	// However, since network updates are not supported, we are not doing that.
	if n.Spec.DriverConfig != nil {
		options = n.Spec.DriverConfig.Options
	}

	ipamData, err := getIpamData(n)
	if err != nil {
		return err
	}
	// now we have everything we need and can call NetworkAllocate. Note that
	// the last param is nil. That param is IPv6 data, but we do not support
	// IPv6
	ds, err := driver.NetworkAllocate(n.ID, options, ipamData, nil)
	if err != nil {
		return errors.ErrInternal("driver.NetworkAllocate call failed: %v", err)
	}

	// finally, update the network object with the driver state we've obtained,
	// and we'll be done and ready to commit
	n.DriverState = &api.Driver{
		Name:    driverName,
		Options: ds,
	}
	return nil
}

// Deallocate frees the network driver state.
func (a *allocator) Deallocate(n *api.Network) error {
	// if the driver state is already nil, we have nothing to do because it was
	// never allocated
	if n.DriverState == nil {
		return nil
	}
	driver, caps, err := a.getDriver(n.DriverState.Name)
	if err != nil {
		return err
	}
	// if the driver is locally scoped, we also have nothing to do here
	if caps.DataScope == datastore.LocalScope {
		return nil
	}
	return driver.NetworkFree(n.ID)
}

// IsNetworkNodeLocal is a helper function that gets the network driver and
// checks its capabilities to see if it is a node-local network.
//
// There is a weird cross-dependency of the IPAM and Driver allocators. The
// IPAM allocator cannot be run on node-local netowrks, but the Driver
// allocator needs IPAM data filled in in order to allocate the network. To
// avoid this, we expose this method, which just returns true if the network is
// node-local, indicating that the IPAM allocator should not be run.
func (a *allocator) IsNetworkNodeLocal(n *api.Network) (bool, error) {
	name := DefaultDriver
	if n.DriverState != nil && n.DriverState.Name != "" {
		name = n.DriverState.Name
	}
	if n.Spec.DriverConfig != nil && n.Spec.DriverConfig.Name != "" {
		name = n.Spec.DriverConfig.Name
	}
	_, caps, err := a.getDriver(name)
	return (caps != nil && caps.DataScope == datastore.LocalScope), err
}

// getDriver retrieves the network driver, attempting to load via the
// PluginGetter if the first attempt to get it from the drvregistry fails
//
// getDriver returns structured errors from the allocator errors package,
// meaning errors it returns can in turn be returned directly from the caller
func (a *allocator) getDriver(name string) (driverapi.Driver, *driverapi.Capability, error) {
	driver, caps := a.drvRegistry.Driver(name)
	// if the first attempt to get the driver fails, it might not be loaded.
	// try again after calling pg.Get, which may load the driver as a side
	// effect
	if driver == nil {
		pg := a.drvRegistry.GetPluginGetter()
		if pg == nil {
			// This case should never happen
			return nil, nil, errors.ErrInternal("plugin store is not initialized")
		}
		_, err := pg.Get(name, driverapi.NetworkPluginEndpointType, plugingetter.Lookup)
		if err != nil {
			// I don't know what would cause this case to happen. will an error
			// be returned if the plugin doesn't exist?
			return nil, nil, errors.ErrInternal("plugingetter.Get failed")
		}
		driver, caps = a.drvRegistry.Driver(name)
		// if the driver still is nil, then it just doesn't exist
		if driver == nil {
			return nil, nil, errors.ErrInvalidSpec("network driver %v cannot be found", name)
		}
	}
	return driver, caps, nil
}

// getIpamData makes driverapi.IPAMData objects (for NetworkAllocate) from the
// network spec
//
// getIpamData returns structured errors from the allocator errors package,
// meaning errors it returns can in turn be returned directly from the caller
func getIpamData(n *api.Network) ([]driverapi.IPAMData, error) {
	ipamData := make([]driverapi.IPAMData, 0, len(n.IPAM.Configs))
	for _, config := range n.IPAM.Configs {
		// we don't support and can do nothing about IPv6.
		if config.Family == api.IPAMConfig_IPV6 {
			continue
		}
		_, subnet, err := net.ParseCIDR(config.Subnet)
		if err != nil {
			// this is another bad error condition, because we should never
			// have committed a network that had an invalid subnet in its ipam
			// configs
			return nil, errors.ErrBadState("cannot parse subnet %v", config.Subnet)
		}
		gwIP := net.ParseIP(config.Gateway)
		gwNet := &net.IPNet{
			IP:   gwIP,
			Mask: subnet.Mask,
		}

		data := driverapi.IPAMData{
			Pool:    subnet,
			Gateway: gwNet,
		}

		ipamData = append(ipamData, data)
	}
	return ipamData, nil
}

// IsAllocated returns true if driver state of the provided network is fully
// allocated.
func IsAllocated(n *api.Network) bool {
	// driver state should not be nil
	// NOTE(dperny): really, the only vital portion of this is DriverState is
	// non-nil and name is not empty. however, including the other checks
	// future-proofs this function a bit.
	return n.DriverState != nil &&
		// driver name should not be empty
		n.DriverState.Name != "" &&
		// either there is a DriverConfig in the spec...
		((n.Spec.DriverConfig != nil &&
			// and the name either matches
			(n.Spec.DriverConfig.Name == n.DriverState.Name ||
				// or is emptystring
				n.Spec.DriverConfig.Name == "")) ||
			// ... or there is no DriverConfig at all at all
			n.Spec.DriverConfig == nil)
}
