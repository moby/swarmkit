package flagparser

import (
	"fmt"
	"strings"

	"github.com/docker/swarmkit/api"
	"github.com/spf13/pflag"
)

// parseNpipe only supports a very simple version of anonymous npipes for
// testing the most basic of data flows. Replace with a --mount flag, similar
// to what we have in docker service.
func parseNpipe(flags *pflag.FlagSet, spec *api.ServiceSpec) error {
	if flags.Changed("npipe") {
		npipes, err := flags.GetStringSlice("npipe")
		if err != nil {
			return err
		}

		container := spec.Task.GetContainer()

		for _, npipe := range npipes {
			parts := strings.SplitN(npipe, ":", 2)
			if len(parts) != 2 {
				return fmt.Errorf("npipe format %q not supported", npipe)
			}
			container.Mounts = append(container.Mounts, api.Mount{
				Type:   api.MountTypeNamedPipe,
				Source: parts[0],
				Target: parts[1],
			})
		}
	}

	return nil
}
