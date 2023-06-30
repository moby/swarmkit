package cnmallocator

import (
	"github.com/docker/docker/libnetwork/drivers/overlay/ovmanager"
	"github.com/docker/docker/libnetwork/drivers/remote"
	"github.com/moby/swarmkit/v2/manager/allocator/networkallocator"
)

var initializers = map[string]driverRegisterFn{
	"remote":  remote.Init,
	"overlay": ovmanager.Init,
}

// PredefinedNetworks returns the list of predefined network structures
func PredefinedNetworks() []networkallocator.PredefinedNetworkData {
	return nil
}
