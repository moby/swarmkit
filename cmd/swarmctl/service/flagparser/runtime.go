package flagparser

import (
	"fmt"

	"github.com/docker/swarmkit/api"
	"github.com/spf13/pflag"
)

func parseRuntime(flags *pflag.FlagSet, spec *api.ServiceSpec) error {
	runtime, err := flags.GetString("runtime")
	if err != nil {
		return err
	}

	// This code can get called when we are initially creating a service or
	// when we are updating it.
	//
	// Make sure a correct empty runtime is created before calling the parse function.
	//
	// We might be creating a runtime for the first time or we might be switching from
	// a runtime to the other.
	switch runtime {
	case "container":
		if spec.Task.GetContainer() == nil {
			spec.Task.Runtime = &api.TaskSpec_Container{
				Container: &api.ContainerSpec{},
			}
		}
		return parseContainer(flags, spec)
	case "plugin":
		if spec.Task.GetPlugin() == nil {
			spec.Task.Runtime = &api.TaskSpec_Plugin{
				Plugin: &api.PluginSpec{},
			}
		}
		return parsePlugin(flags, spec)
	default:
		return fmt.Errorf("invalid runtime: %q", runtime)
	}
}
