// +build !linux,!darwin,!windows

package network

import (
	"github.com/docker/swarmkit/manager/allocator/networkallocator"
)

// NOTE(dperny): these driver names are also indepedently in the helpers
// package. If you add or remote drivers, you need to do so there as well.

const initializers = nil

// PredefinedNetworks returns the list of predefined network structures
func PredefinedNetworks() []PredefinedNetworkData {
	return nil
}
