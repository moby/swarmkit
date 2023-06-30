package cnmallocator

import (
	"github.com/docker/docker/libnetwork/drivers/overlay/ovmanager"
	"github.com/docker/docker/libnetwork/drivers/remote"
	"github.com/docker/docker/libnetwork/drvregistry"
	"github.com/moby/swarmkit/v2/manager/allocator/networkallocator"
)

var initializers = map[string]drvregistry.InitFunc{
	"remote":   remote.Init,
	"overlay":  ovmanager.Init,
	"internal": StubManagerInit("internal"),
	"l2bridge": StubManagerInit("l2bridge"),
	"nat":      StubManagerInit("nat"),
}

// PredefinedNetworks returns the list of predefined network structures
func PredefinedNetworks() []networkallocator.PredefinedNetworkData {
	return []networkallocator.PredefinedNetworkData{
		{Name: "nat", Driver: "nat"},
	}
}
