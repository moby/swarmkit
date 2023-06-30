package cnmallocator

import (
	"github.com/docker/docker/libnetwork/drivers/overlay/ovmanager"
	"github.com/moby/swarmkit/v2/manager/allocator/networkallocator"
)

var initializers = map[string]driverRegisterFn{
	"remote":   registerRemote,
	"overlay":  ovmanager.Register,
	"internal": registerNetworkType("internal"),
	"l2bridge": registerNetworkType("l2bridge"),
	"nat":      registerNetworkType("nat"),
}

// PredefinedNetworks returns the list of predefined network structures
func PredefinedNetworks() []networkallocator.PredefinedNetworkData {
	return []networkallocator.PredefinedNetworkData{
		{Name: "nat", Driver: "nat"},
	}
}
