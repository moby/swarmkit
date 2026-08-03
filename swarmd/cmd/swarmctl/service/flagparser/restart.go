package flagparser

import (
	"fmt"
	"time"

	"github.com/moby/swarmkit/v2/api"
	"github.com/spf13/pflag"
	durationpb "google.golang.org/protobuf/types/known/durationpb"
)

func parseRestart(flags *pflag.FlagSet, spec *api.ServiceSpec) error {
	if spec.Task.Restart == nil {
		// set new service's restart policy as RestartOnAny
		spec.Task.Restart = &api.RestartPolicy{
			Condition: api.RestartPolicy_ANY,
		}
	}

	if flags.Changed("restart-condition") {
		condition, err := flags.GetString("restart-condition")
		if err != nil {
			return err
		}

		switch condition {
		case "none":
			spec.Task.Restart.Condition = api.RestartPolicy_NONE
		case "failure":
			spec.Task.Restart.Condition = api.RestartPolicy_ON_FAILURE
		case "any":
			spec.Task.Restart.Condition = api.RestartPolicy_ANY
		default:
			return fmt.Errorf("invalid restart condition: %s", condition)
		}
	}

	if flags.Changed("restart-delay") {
		delay, err := flags.GetString("restart-delay")
		if err != nil {
			return err
		}

		delayDuration, err := time.ParseDuration(delay)
		if err != nil {
			return err
		}

		spec.Task.Restart.Delay = durationpb.New(delayDuration)
	}

	if flags.Changed("restart-max-attempts") {
		attempts, err := flags.GetUint64("restart-max-attempts")
		if err != nil {
			return err
		}

		spec.Task.Restart.MaxAttempts = attempts
	}

	if flags.Changed("restart-window") {
		window, err := flags.GetString("restart-window")
		if err != nil {
			return err
		}

		windowDelay, err := time.ParseDuration(window)
		if err != nil {
			return err
		}

		spec.Task.Restart.Window = durationpb.New(windowDelay)
	}

	return nil
}
