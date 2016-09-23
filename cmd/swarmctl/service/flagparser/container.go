package flagparser

import (
	"github.com/docker/swarmkit/api"
	"github.com/spf13/pflag"
)

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

	if flags.Changed("cap-add") {
		caps, err := flags.GetStringSlice("cap-add")
		if err != nil {
			return err
		}
		spec.Task.GetContainer().CapAdd = caps
	}

	if flags.Changed("cap-drop") {
		caps, err := flags.GetStringSlice("cap-drop")
		if err != nil {
			return err
		}
		spec.Task.GetContainer().CapDrop = caps
	}

	return nil
}
