package flagparser

import (
	"fmt"

	"github.com/moby/swarmkit/v2/api"
	"github.com/spf13/pflag"
)

func parsePlacement(flags *pflag.FlagSet, spec *api.ServiceSpec) error {
	if flags.Changed("constraint") {
		constraints, err := flags.GetStringSlice("constraint")
		if err != nil {
			return err
		}
		if spec.Task.Placement == nil {
			spec.Task.Placement = &api.Placement{}
		}
		spec.Task.Placement.Constraints = constraints
	}

	if flags.Changed("replicas-max-per-node") {
		if spec.GetReplicated() == nil {
			return fmt.Errorf("--replicas-max-per-node can only be specified in --mode replicated")
		}
		maxReplicas, err := flags.GetUint64("replicas-max-per-node")
		if err != nil {
			return err
		}
		if spec.Task.Placement == nil {
			spec.Task.Placement = &api.Placement{}
		}
		spec.Task.Placement.MaxReplicas = maxReplicas
	}

	return nil
}
