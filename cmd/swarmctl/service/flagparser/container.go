package flagparser

import (
	"github.com/docker/swarmkit/api"
	"github.com/spf13/pflag"
)

func parseContainer(flags *pflag.FlagSet, spec *api.ServiceSpec) error {
	if flags.Changed("env") {
		env, err := flags.GetStringSlice("env")
		if err != nil {
			return err
		}
		spec.Task.GetContainer().Env = env
	}

	return nil
}
