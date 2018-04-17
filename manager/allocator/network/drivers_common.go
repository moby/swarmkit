package network

import (
	"github.com/docker/libnetwork/drvregistry"
)

// types common to all driver_platform files

// initializer represents the information needed to initialize a network driver
// it is used in the platform-specific driver.go files
type initializer struct {
	fn    drvregistry.InitFunc
	ntype string
}
