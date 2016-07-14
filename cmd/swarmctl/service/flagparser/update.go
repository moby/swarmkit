package flagparser

import (
	"time"

	"github.com/docker/swarmkit/api"
	"github.com/docker/swarmkit/protobuf/ptypes"
	"github.com/spf13/pflag"
)

func parseUpdate(flags *pflag.FlagSet, spec *api.ServiceSpec) error {
	if flags.Changed("update-parallelism") {
		parallelism, err := flags.GetUint64("update-parallelism")
		if err != nil {
			return err
		}
		if spec.Update == nil {
			spec.Update = &api.UpdateConfig{}
		}
		spec.Update.Parallelism = parallelism
	}

	if flags.Changed("update-delay") {
		delay, err := flags.GetString("update-delay")
		if err != nil {
			return err
		}

		delayDuration, err := time.ParseDuration(delay)
		if err != nil {
			return err
		}

		if spec.Update == nil {
			spec.Update = &api.UpdateConfig{}
		}
		spec.Update.Delay = *ptypes.DurationProto(delayDuration)
	}

	if flags.Changed("update-pre-allocate") {
		preAllocate, err := flags.GetBool("update-pre-allocate")
		if err != nil {
			return err
		}

		if spec.Update == nil {
			spec.Update = &api.UpdateConfig{}
		}
		spec.Update.PreAllocate = preAllocate
	}
	return nil
}
