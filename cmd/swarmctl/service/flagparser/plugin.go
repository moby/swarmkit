package flagparser

import (
	"github.com/docker/swarmkit/api"
	"github.com/spf13/pflag"
)

func parsePlugin(flags *pflag.FlagSet, spec *api.ServiceSpec) error {
	if flags.Changed("image") {
		image, err := flags.GetString("image")
		if err != nil {
			return err
		}
		spec.Task.GetPlugin().Image = image
	}

	return nil
}
