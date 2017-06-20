package cnmallocator

import (
	"github.com/docker/libnetwork/drivers/overlay/ovmanager"
	"github.com/docker/libnetwork/drivers/remote"
	"github.com/docker/libnetwork/drivers/windows"
	"github.com/docker/swarmkit/manager/allocator/networkallocator"
)

var initializers = []initializer{
	{remote.Init, "remote"},
	{ovmanager.Init, "overlay"},
	{windows.GetInit("transparent"), "transparent"},
	{windows.GetInit("l2bridge"), "l2bridge"},
	{windows.GetInit("l2tunnel"), "l2tunnel"},
	{windows.GetInit("nat"), "nat"},
}

// PredefinedNetworks returns the list of predefined network structures
func PredefinedNetworks() []networkallocator.PredefinedNetworkData {
	return nil
}
