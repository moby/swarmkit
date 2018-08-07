package network

import (
	"github.com/docker/libnetwork/drvregistry"
	"github.com/docker/swarmkit/manager/allocator/helpers"
)

// types common to all driver_platform files

// initializer represents the information needed to initialize a network driver
// it is used in the platform-specific driver.go files
type initializer struct {
	fn    drvregistry.InitFunc
	ntype string
}

var (
	// PredefinedLabel identifies internally allocated swarm networks
	// corresponding to the node-local predefined networks on the host.
	PredefinedLabel = helpers.PredefinedLabel
)

// PredefinedNetworkData contains the minimum set of data needed
// to create the correspondent predefined network object in the store.
type PredefinedNetworkData struct {
	Name   string
	Driver string
}
