package flagparser

import (
	"fmt"
	"strings"

	"github.com/docker/swarmkit/api"
	"github.com/spf13/pflag"
)

func parseContainerDevices(flags *pflag.FlagSet, container *api.ContainerSpec) error {
	devices, err := flags.GetStringSlice("device")
	if err != nil {
		return err
	}

	for _, device := range devices {
		if strings.Contains(device, ":") {
			return fmt.Errorf("device format %q not supported", device)
		}
		container.Devices = append(container.Devices, &api.Device{
			Source:      device,
			Target:      device,
			Permissions: "rwm",
		})
	}

	return nil
}

func parseContainer(flags *pflag.FlagSet, spec *api.ServiceSpec) error {
	if flags.Changed("image") {
		image, err := flags.GetString("image")
		if err != nil {
			return err
		}
		spec.Task.GetContainer().Image = image
	}

	if flags.Changed("command") {
		command, err := flags.GetStringSlice("command")
		if err != nil {
			return err
		}
		spec.Task.GetContainer().Command = command
	}

	if flags.Changed("args") {
		args, err := flags.GetStringSlice("args")
		if err != nil {
			return err
		}
		spec.Task.GetContainer().Args = args
	}

	if flags.Changed("env") {
		env, err := flags.GetStringSlice("env")
		if err != nil {
			return err
		}
		spec.Task.GetContainer().Env = env
	}

	if flags.Changed("device") {
		if err := parseContainerDevices(flags, spec.Task.GetContainer()); err != nil {
			return err
		}
	}

	return nil
}
