package networker

import (
	"fmt"
	"net"

	"github.com/Sirupsen/logrus"
	"github.com/docker/libnetwork/driverapi"
	"github.com/docker/libnetwork/drivers/overlay/ovmanager"
	"github.com/docker/libnetwork/drvregistry"
	"github.com/docker/libnetwork/ipamapi"
	"github.com/docker/swarm-v2/api"
)

const (
	defaultDriver = "overlay"
)

var (
	defaultDriverInitFunc = ovmanager.Init
)

// Networker acts as the controller for all network related operations
// like managing network and IPAM drivers and also creating and
// deleting networks and the associated resources.
type Networker struct {
	// The driver register which manages all internal and external
	// IPAM and network drivers.
	drvRegistry *drvregistry.DrvRegistry

	// Local network state used by Networker to do network management.
	networks map[string]*network
}

// Local in-memory state related to netwok that need to be tracked by Networker
type network struct {
	id string

	// pools is used to save the internal poolIDs needed when
	// releasing the pool.
	pools map[string]string

	// endpoints is a map of endpoint IP to the poolID from which it
	// was allocated.
	endpoints map[string]string
}

// New returns a new Networker handle
func New() (*Networker, error) {
	nwkr := &Networker{
		networks: make(map[string]*network),
	}

	// There are no driver configurations and notification
	// functions as of now.
	reg, err := drvregistry.New(nil, nil, nil, nil)
	if err != nil {
		return nil, err
	}

	// Add the manager component of overlay driver to the registry.
	if err := reg.AddDriver(defaultDriver, defaultDriverInitFunc, nil); err != nil {
		return nil, err
	}

	nwkr.drvRegistry = reg
	return nwkr, nil
}

// NetworkAllocate allocates all the necessary resources both general
// and driver-specific which may be specified in the NetworkSpec
func (nwkr *Networker) NetworkAllocate(n *api.Network) error {
	if _, ok := nwkr.networks[n.ID]; ok {
		return fmt.Errorf("network %s already allocated", n.ID)
	}

	pools, err := nwkr.allocatePools(n)
	if err != nil {
		return fmt.Errorf("failed allocating pools and gateway IP for network %s: %v", n.ID, err)
	}

	if err := nwkr.allocateDriverState(n); err != nil {
		return fmt.Errorf("failed while allocating driver state for network %s: %v", n.ID, err)
	}

	nwkr.networks[n.ID] = &network{
		id:        n.ID,
		pools:     pools,
		endpoints: make(map[string]string),
	}

	return nil
}

func (nwkr *Networker) getNetwork(id string) *network {
	return nwkr.networks[id]
}

// NetworkFree frees all the general and driver specific resources
// whichs were assigned to the passed network.
func (nwkr *Networker) NetworkFree(n *api.Network) error {
	localNet := nwkr.getNetwork(n.ID)
	if localNet == nil {
		return fmt.Errorf("could not get networker state for network %s", n.ID)
	}

	if err := nwkr.freeDriverState(n); err != nil {
		return fmt.Errorf("failed to free driver state for network %s: %v", n.ID, err)
	}

	delete(nwkr.networks, n.ID)
	return nwkr.freePools(n, localNet.pools)
}

// EndpointsAllocate allocates all the endpoint resources for all the
// networks that a task is attached to.
func (nwkr *Networker) EndpointsAllocate(t *api.Task) error {
	for i, na := range t.Networks {
		ipam, _, err := nwkr.resolveIPAM(na.Network)
		if err != nil {
			return fmt.Errorf("failed to resolve IPAM while allocating : %v", err)
		}

		localNet := nwkr.getNetwork(na.Network.ID)
		if localNet == nil {
			return fmt.Errorf("could not find networker state")
		}

		if err := nwkr.allocateNetworkIPs(na, ipam, localNet); err != nil {
			if err := nwkr.releaseEndpoints(t.Networks[:i]); err != nil {
				logrus.Errorf("Failed to release IP addresses while rolling back allocation for task %s network %s: %v", t.ID, na.Network.ID, err)
			}
			return fmt.Errorf("failed to allocate network IP for task %s network %s: %v", t.ID, na.Network.ID, err)
		}
	}

	return nil
}

// EndpointsFree releases all the endpoint resources for all the
// networks that a task is attached to.
func (nwkr *Networker) EndpointsFree(t *api.Task) error {
	return nwkr.releaseEndpoints(t.Networks)
}

func (nwkr *Networker) releaseEndpoints(networks []*api.Task_NetworkAttachment) error {
	for _, na := range networks {
		ipam, _, err := nwkr.resolveIPAM(na.Network)
		if err != nil {
			return fmt.Errorf("failed to resolve IPAM while allocating : %v", err)
		}

		localNet := nwkr.getNetwork(na.Network.ID)
		if localNet == nil {
			return fmt.Errorf("could not find networker state")
		}

		// Do not fail and bail out if we fail to release IP
		// address here. Keep going and try releasing as many
		// addresses as possible.
		for _, addr := range na.Addresses {
			// Retrieve the poolID and immediately nuke
			// out the mapping.
			poolID := localNet.endpoints[addr]
			delete(localNet.endpoints, addr)

			ip, _, err := net.ParseCIDR(addr)
			if err != nil {
				logrus.Errorf("Could not parse IP address %s while releasing", addr)
				continue
			}

			if err := ipam.ReleaseAddress(poolID, ip); err != nil {
				logrus.Errorf("IPAM failure while releasing IP address %s: %v", addr, err)
			}
		}

		// Clear out the address list when we are done with
		// this network.
		na.Addresses = nil
	}

	return nil
}

// allocate the endpoint IP addresses for a single network attachment of the task.
func (nwkr *Networker) allocateNetworkIPs(na *api.Task_NetworkAttachment, ipam ipamapi.Ipam, localNet *network) error {
	var ip *net.IPNet
	for _, poolID := range localNet.pools {
		var err error

		ip, _, err = ipam.RequestAddress(poolID, nil, nil)
		if err != nil && err != ipamapi.ErrNoAvailableIPs {
			return fmt.Errorf("could not allocate IP from IPAM: %v", err)
		}

		// If we got an address then we are done.
		if err == nil {
			ipStr := ip.String()
			localNet.endpoints[ipStr] = poolID
			na.Addresses = append(na.Addresses, ipStr)
			return nil
		}
	}

	return fmt.Errorf("could not find an available IP")
}

func (nwkr *Networker) freeDriverState(n *api.Network) error {
	d, _, err := nwkr.resolveDriver(n)
	if err != nil {
		return err
	}

	return d.NetworkFree(n.ID)
}

func (nwkr *Networker) allocateDriverState(n *api.Network) error {
	d, dName, err := nwkr.resolveDriver(n)
	if err != nil {
		return err
	}

	// Construct IPAM data for driver consumption.
	ipv4Data := make([]driverapi.IPAMData, 0, len(n.Spec.IPAM.IPv4))
	for _, ic := range n.Spec.IPAM.IPv4 {
		_, subnet, err := net.ParseCIDR(ic.Subnet)
		if err != nil {
			return fmt.Errorf("error parsing subnet %s while allocating driver state: %v", ic.Subnet, err)
		}

		gwIP := net.ParseIP(ic.Gateway)
		gwNet := &net.IPNet{
			IP:   gwIP,
			Mask: subnet.Mask,
		}

		data := driverapi.IPAMData{
			Pool:    subnet,
			Gateway: gwNet,
		}

		ipv4Data = append(ipv4Data, data)
	}

	ds, err := d.NetworkAllocate(n.ID, nil, ipv4Data, nil)
	if err != nil {
		return err
	}

	// Update network object with the obtained driver state.
	n.DriverState = &api.Driver{
		Name:    dName,
		Options: ds,
	}

	return nil
}

// Resolve network driver
func (nwkr *Networker) resolveDriver(n *api.Network) (driverapi.Driver, string, error) {
	dName := defaultDriver
	if n.Spec.DriverConfiguration != nil && n.Spec.DriverConfiguration.Name != "" {
		dName = n.Spec.DriverConfiguration.Name
	}

	d, _ := nwkr.drvRegistry.Driver(dName)
	if d == nil {
		return nil, "", fmt.Errorf("could not resolve network driver %s", dName)
	}

	return d, dName, nil
}

// Resolve the IPAM driver
func (nwkr *Networker) resolveIPAM(n *api.Network) (ipamapi.Ipam, string, error) {
	dName := ipamapi.DefaultIPAM
	if n.Spec.IPAM != nil && n.Spec.IPAM.Driver != nil && n.Spec.IPAM.Driver.Name != "" {
		dName = n.Spec.IPAM.Driver.Name
	}

	ipam, _ := nwkr.drvRegistry.IPAM(dName)
	if ipam == nil {
		return nil, "", fmt.Errorf("could not resolve IPAM driver %s", dName)
	}

	return ipam, dName, nil
}

func (nwkr *Networker) freePools(n *api.Network, pools map[string]string) error {
	ipam, _, err := nwkr.resolveIPAM(n)
	if err != nil {
		return fmt.Errorf("failed to resolve IPAM while freeing pools for network %s: %v", n.ID, err)
	}

	releasePools(ipam, n.Spec.IPAM.IPv4, pools)
	return nil
}

func releasePools(ipam ipamapi.Ipam, icList []*api.IPAMConfiguration, pools map[string]string) {
	for _, ic := range icList {
		if err := ipam.ReleaseAddress(pools[ic.Subnet], net.ParseIP(ic.Gateway)); err != nil {
			logrus.Errorf("Failed to release address %s: %v", ic.Subnet, err)
		}
	}

	for k, p := range pools {
		if err := ipam.ReleasePool(p); err != nil {
			logrus.Errorf("Failed to release pool %s: %v", k, err)
		}
	}
}

func (nwkr *Networker) allocatePools(n *api.Network) (map[string]string, error) {
	ipam, dName, err := nwkr.resolveIPAM(n)
	if err != nil {
		return nil, err
	}

	// We don't support user defined address spaces yet so just
	// retrive default address space names for the driver.
	_, asName, err := nwkr.drvRegistry.IPAMDefaultAddressSpaces(dName)
	if err != nil {
		return nil, err
	}

	pools := make(map[string]string)

	if n.Spec.IPAM == nil {
		n.Spec.IPAM = &api.NetworkSpec_IPAMOptions{}
	}

	ipamConfigs := n.Spec.IPAM.IPv4
	if n.Spec.IPAM.IPv4 == nil {
		// Auto-generate new IPAM configuration if the user
		// provided no configuration.
		ipamConfigs = append(ipamConfigs, &api.IPAMConfiguration{})
		n.Spec.IPAM.IPv4 = ipamConfigs
	}

	for i, ic := range ipamConfigs {
		poolID, poolIP, _, err := ipam.RequestPool(asName, ic.Subnet, ic.Range, nil, false)
		if err != nil {
			// Rollback by releasing all the resources allocated so far.
			releasePools(ipam, ipamConfigs[:i], pools)
			return nil, err
		}
		pools[poolIP.String()] = poolID

		gwIP, _, err := ipam.RequestAddress(poolID, net.ParseIP(ic.Gateway), nil)
		if err != nil {
			// Rollback by releasing all the resources allocated so far.
			releasePools(ipam, ipamConfigs[:i], pools)
			return nil, err
		}

		if ic.Subnet == "" {
			ic.Subnet = poolIP.String()
			ic.Gateway = gwIP.IP.String()
		}
	}

	return pools, nil
}
