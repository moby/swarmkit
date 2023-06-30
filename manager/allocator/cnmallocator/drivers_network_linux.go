package cnmallocator

import (
	"github.com/docker/docker/libnetwork/drivers/bridge/brmanager"
	"github.com/docker/docker/libnetwork/drivers/host"
	"github.com/docker/docker/libnetwork/drivers/ipvlan/ivmanager"
	"github.com/docker/docker/libnetwork/drivers/macvlan/mvmanager"
	"github.com/docker/docker/libnetwork/drivers/overlay/ovmanager"
	"github.com/docker/docker/libnetwork/drivers/remote"
	"github.com/docker/docker/libnetwork/drvregistry"
	"github.com/moby/swarmkit/v2/manager/allocator/networkallocator"
)

var initializers = map[string]drvregistry.InitFunc{
	"remote":  remote.Init,
	"overlay": ovmanager.Init,
	"macvlan": mvmanager.Init,
	"bridge":  brmanager.Init,
	"ipvlan":  ivmanager.Init,
	"host":    host.Init,
}

// PredefinedNetworks returns the list of predefined network structures
func PredefinedNetworks() []networkallocator.PredefinedNetworkData {
	return []networkallocator.PredefinedNetworkData{
		{Name: "bridge", Driver: "bridge"},
		{Name: "host", Driver: "host"},
	}
}
