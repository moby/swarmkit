package drvregistry

import (
	"fmt"
	"strings"
	"sync"

	"github.com/docker/libnetwork/driverapi"
	"github.com/docker/libnetwork/ipamapi"
	"github.com/docker/libnetwork/types"

	builtinIpam "github.com/docker/libnetwork/ipams/builtin"
	remoteIpam "github.com/docker/libnetwork/ipams/remote"
)

type driverData struct {
	driver     driverapi.Driver
	capability driverapi.Capability
}

type ipamData struct {
	driver     ipamapi.Ipam
	capability *ipamapi.Capability
	// default address spaces are provided by ipam driver at registration time
	defaultLocalAddressSpace, defaultGlobalAddressSpace string
}

type driverTable map[string]*driverData
type ipamTable map[string]*ipamData

type DrvRegistry struct {
	sync.Mutex
	drivers     driverTable
	ipamDrivers ipamTable
	dfn         DriverNotifyFunc
	ifn         IPAMNotifyFunc
}

// Functors definition
type InitFunc func(driverapi.DriverCallback, map[string]interface{}) error
type IPAMWalkFunc func(name string, driver ipamapi.Ipam, cap *ipamapi.Capability) bool
type DriverWalkFunc func(name string, driver driverapi.Driver, capability driverapi.Capability) bool
type IPAMNotifyFunc func(name string, driver ipamapi.Ipam, cap *ipamapi.Capability) error
type DriverNotifyFunc func(name string, driver driverapi.Driver, capability driverapi.Capability) error

func New(lDs, gDs interface{}, dfn DriverNotifyFunc, ifn IPAMNotifyFunc) (*DrvRegistry, error) {
	r := &DrvRegistry{
		drivers:     make(driverTable),
		ipamDrivers: make(ipamTable),
		dfn:         dfn,
		ifn:         ifn,
	}

	if err := r.initIPAMs(lDs, gDs); err != nil {
		return nil, err
	}

	return r, nil
}

func (r *DrvRegistry) AddDriver(ntype string, fn InitFunc, config map[string]interface{}) error {
	return fn(r, config)
}

func (r *DrvRegistry) WalkIPAMs(ifn IPAMWalkFunc) {
	type ipamVal struct {
		name string
		data *ipamData
	}

	r.Lock()
	ivl := make([]ipamVal, 0, len(r.ipamDrivers))
	for k, v := range r.ipamDrivers {
		ivl = append(ivl, ipamVal{name: k, data: v})
	}
	r.Unlock()

	for _, iv := range ivl {
		if ifn(iv.name, iv.data.driver, iv.data.capability) {
			break
		}
	}
}

func (r *DrvRegistry) WalkDrivers(dfn DriverWalkFunc) {
	type driverVal struct {
		name string
		data *driverData
	}

	r.Lock()
	dvl := make([]driverVal, 0, len(r.drivers))
	for k, v := range r.drivers {
		dvl = append(dvl, driverVal{name: k, data: v})
	}
	r.Unlock()

	for _, dv := range dvl {
		if dfn(dv.name, dv.data.driver, dv.data.capability) {
			break
		}
	}
}

func (r *DrvRegistry) Driver(name string) (driverapi.Driver, *driverapi.Capability) {
	r.Lock()
	defer r.Unlock()

	d, ok := r.drivers[name]
	if !ok {
		return nil, nil
	}

	return d.driver, &d.capability
}

func (r *DrvRegistry) IPAM(name string) (ipamapi.Ipam, *ipamapi.Capability) {
	r.Lock()
	defer r.Unlock()

	i, ok := r.ipamDrivers[name]
	if !ok {
		return nil, nil
	}

	return i.driver, i.capability
}

func (r *DrvRegistry) IPAMDefaultAddressSpaces(name string) (string, string, error) {
	r.Lock()
	defer r.Unlock()

	i, ok := r.ipamDrivers[name]
	if !ok {
		return "", "", fmt.Errorf("ipam %s not found", name)
	}

	return i.defaultLocalAddressSpace, i.defaultGlobalAddressSpace, nil
}

func (r *DrvRegistry) initIPAMs(lDs, gDs interface{}) error {
	for _, fn := range [](func(ipamapi.Callback, interface{}, interface{}) error){
		builtinIpam.Init,
		remoteIpam.Init,
	} {
		if err := fn(r, lDs, gDs); err != nil {
			return err
		}
	}

	return nil
}

func (r *DrvRegistry) RegisterDriver(ntype string, driver driverapi.Driver, capability driverapi.Capability) error {
	if strings.TrimSpace(ntype) == "" {
		return fmt.Errorf("network type string cannot be empty")
	}

	r.Lock()
	_, ok := r.drivers[ntype]
	r.Unlock()

	if ok {
		return driverapi.ErrActiveRegistration(ntype)
	}

	if r.dfn != nil {
		if err := r.dfn(ntype, driver, capability); err != nil {
			return err
		}
	}

	dData := &driverData{driver, capability}

	r.Lock()
	r.drivers[ntype] = dData
	r.Unlock()

	return nil
}

func (r *DrvRegistry) registerIpamDriver(name string, driver ipamapi.Ipam, caps *ipamapi.Capability) error {
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("ipam driver name string cannot be empty")
	}

	r.Lock()
	_, ok := r.ipamDrivers[name]
	r.Unlock()
	if ok {
		return types.ForbiddenErrorf("ipam driver %q already registered", name)
	}

	locAS, glbAS, err := driver.GetDefaultAddressSpaces()
	if err != nil {
		return types.InternalErrorf("ipam driver %q failed to return default address spaces: %v", name, err)
	}

	if r.ifn != nil {
		if err := r.ifn(name, driver, caps); err != nil {
			return err
		}
	}

	r.Lock()
	r.ipamDrivers[name] = &ipamData{driver: driver, defaultLocalAddressSpace: locAS, defaultGlobalAddressSpace: glbAS, capability: caps}
	r.Unlock()

	return nil
}

func (r *DrvRegistry) RegisterIpamDriver(name string, driver ipamapi.Ipam) error {
	return r.registerIpamDriver(name, driver, &ipamapi.Capability{})
}

func (r *DrvRegistry) RegisterIpamDriverWithCapabilities(name string, driver ipamapi.Ipam, caps *ipamapi.Capability) error {
	return r.registerIpamDriver(name, driver, caps)
}
