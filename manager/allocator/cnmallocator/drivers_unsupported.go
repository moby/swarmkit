// +build !linux,!darwin,!windows

package cnmallocator

import (
	"github.com/docker/swarmkit/manager/allocator/networkallocator"
)

const initializers = nil

// PredefinedNetworks returns the list of predefined network structures
func PredefinedNetworks() []networkallocator.PredefinedNetworkData {
	return nil
}

// IsPredefinedNetwork checks if the network is a host/bridge network
func IsPredefinedNetwork(target string) bool {
	return false
}
