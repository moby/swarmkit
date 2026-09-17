package flagparser

import (
	"strings"

	"github.com/moby/swarmkit/v2/api"
	"github.com/pkg/errors"
	"github.com/spf13/pflag"
)

func parseDevice(flags *pflag.FlagSet, spec *api.ServiceSpec) error {
	if flags.Changed("device") {
		container := spec.Task.GetContainer()
		if container == nil {
			return nil
		}

		devices, err := flags.GetStringSlice("device")
		if err != nil {
			return err
		}

		container.Devices = make([]*api.ContainerSpec_DeviceMapping, len(devices))

		for i, device := range devices {
			parts := strings.Split(device, ":")
			if len(parts) < 1 || len(parts) > 3 {
				return errors.Wrap(err, "failed to parse device")
			}

			mapping := &api.ContainerSpec_DeviceMapping{
				PathOnHost:      parts[0],
				PathInContainer: parts[0],
			}

			if len(parts) > 1 {
				mapping.PathInContainer = parts[1]
			}

			if len(parts) == 3 {
				mapping.CgroupPermissions = parts[2]
			}

			container.Devices[i] = mapping
		}
	}

	return nil
}
