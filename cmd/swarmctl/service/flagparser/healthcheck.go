package flagparser

import (
	"fmt"

	"github.com/docker/swarmkit/api"
	"github.com/docker/swarmkit/protobuf/ptypes"
	"github.com/spf13/pflag"
)

func parseHealthCheck(flags *pflag.FlagSet, spec *api.ServiceSpec) error {

	haveHealthSettings := flags.Changed("health-cmd") ||
		flags.Changed("health-interval") ||
		flags.Changed("health-timeout") ||
		flags.Changed("health-retries")

	noHealthCheck, err := flags.GetBool("no-healthcheck")
	if err != nil {
		return err
	}
	if noHealthCheck {
		if haveHealthSettings {
			return fmt.Errorf("--no-healthcheck conflicts with --health-* options")
		}
		test := []string{"NONE"}
		spec.Task.GetContainer().HealthCheck = &api.HealthConfig{Test: test}
	} else if !haveHealthSettings {
		return nil
	}

	healthCMD, err := flags.GetString("health-cmd")
	if err != nil {
		return err
	}
	var args []string
	if healthCMD != "" {
		args = []string{"CMD-SHELL", healthCMD}
	}

	healthInterval, err := flags.GetDuration("health-interval")
	if err != nil {
		return err
	}
	if healthInterval < 0 {
		return fmt.Errorf("--health-interval cannot be negative")
	}

	healthTimeout, err := flags.GetDuration("health-timeout")
	if err != nil {
		return err
	}
	if healthTimeout < 0 {
		return fmt.Errorf("--health-timeout cannot be negative")
	}

	healthRetries, err := flags.GetInt("health-retries")
	if err != nil {
		return err
	}

	spec.Task.GetContainer().HealthCheck = &api.HealthConfig{
		Test:     args,
		Interval: ptypes.DurationProto(healthInterval),
		Timeout:  ptypes.DurationProto(healthTimeout),
		Retries:  int32(healthRetries),
	}

	return nil
}
