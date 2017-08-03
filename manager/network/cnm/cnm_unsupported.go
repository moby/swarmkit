// +build !linux,!darwin,!windows

package cnm

import (
	"github.com/docker/swarmkit/manager/network"
)

// PredefinedNetworks returns the list of predefined network structures
func (nm *cnm) PredefinedNetworks() []network.PredefinedNetworkData {
	return nil
}
