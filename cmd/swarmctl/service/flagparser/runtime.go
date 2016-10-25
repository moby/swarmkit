package flagparser

import (
	"github.com/docker/swarmkit/api"
	"github.com/spf13/pflag"
)

func parseRuntime(flags *pflag.FlagSet, spec *api.ServiceSpec) error {
	if flags.Changed("runtime") {
		runtime, err := flags.GetString("runtime")
		if err != nil {
			return err
		}

		switch runtime {
		case "container":
			return parseContainer(flags, spec)
		case "plugin":
			return parsePlugin(flags, spec)
		}
	}
	return nil
}
