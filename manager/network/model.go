package network

import (
	"github.com/docker/swarmkit/api"
	"github.com/docker/swarmkit/manager/allocator/networkallocator"
)

// Model is an abstraction over the Network Model to be used.
type Model interface {
	NewAllocator() (networkallocator.NetworkAllocator, error)
	ValidateDriver(driver *api.Driver, pluginType string) error
}
