package network

import (
	"github.com/docker/swarmkit/api"
	"github.com/docker/swarmkit/manager/allocator/networkallocator"
)

// Model is an abstraction over the Network Model to be used.
type Model interface {
	NewAllocator() (networkallocator.NetworkAllocator, error)
	ValidateDriver(driver *api.Driver, pluginType string) error
	PredefinedNetworks() []PredefinedNetworkData
}

// PredefinedNetworkData contains the minimum set of data needed
// to create the correspondent predefined network object in the store.
type PredefinedNetworkData struct {
	Name   string
	Driver string
}
