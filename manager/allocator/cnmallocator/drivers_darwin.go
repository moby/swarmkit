package cnmallocator

import (
	"github.com/docker/docker/libnetwork/drivers/overlay/ovmanager"
	"github.com/moby/swarmkit/v2/manager/allocator/networkallocator"
)

var initializers = map[string]driverRegisterFn{
	"remote":  registerRemote,
	"overlay": ovmanager.Register,
}

// PredefinedNetworks returns the list of predefined network structures
func PredefinedNetworks() []networkallocator.PredefinedNetworkData {
	return nil
}
