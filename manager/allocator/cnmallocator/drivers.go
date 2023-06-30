package cnmallocator

import (
	"fmt"

	"github.com/docker/docker/libnetwork/driverapi"
	"github.com/docker/docker/libnetwork/drivers/remote"
)

type driverRegisterFn func(r driverapi.Registerer, config map[string]interface{}) error

func registerRemote(r driverapi.Registerer, _ map[string]interface{}) error {
	dc, ok := r.(driverapi.DriverCallback)
	if !ok {
		return fmt.Errorf(`failed to register "remote" driver: driver does not implement driverapi.DriverCallback`)
	}
	return remote.Register(dc, dc.GetPluginGetter())
}

//nolint:unused // is currently only used on Windows, but keeping these adaptors together in one file.
func registerNetworkType(networkType string) func(dc driverapi.Registerer, config map[string]interface{}) error {
	return func(r driverapi.Registerer, _ map[string]interface{}) error {
		return RegisterManager(r, networkType)
	}
}
