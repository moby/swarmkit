package flagparser

import (
	"fmt"
	"time"
	"errors"

	"github.com/docker/swarmkit/api"
	gogotypes "github.com/gogo/protobuf/types"
	"github.com/spf13/pflag"
)

func parseRestart(flags *pflag.FlagSet, spec *api.ServiceSpec) error {
	if spec.Task.Restart == nil {
		// set new service's restart policy as RestartOnAny
		spec.Task.Restart = &api.RestartPolicy{
			Condition: api.RestartOnAny,
		}
	}

	if flags.Changed("restart-condition") {
		condition, err := flags.GetString("restart-condition")
		if err != nil {
			return err
		}

		switch condition {
		case "none":
			spec.Task.Restart.Condition = api.RestartOnNone
		case "failure":
			spec.Task.Restart.Condition = api.RestartOnFailure
		case "any":
			spec.Task.Restart.Condition = api.RestartOnAny
		default:
			return fmt.Errorf("invalid restart condition: %s", condition)
		}
	}

	var restartDelay bool
	if restartDelay := flags.Changed("restart-delay"); restartDelay {
		delay, err := flags.GetString("restart-delay")
		if err != nil {
			return err
		}

		delayDuration, err := time.ParseDuration(delay)
		if err != nil {
			return err
		}

		spec.Task.Restart.Delay = gogotypes.DurationProto(delayDuration)
	}

	if flags.Changed("restart-backoff-base") {
		if restartDelay {
			return errors.New("restart-backoff-base is not compatible with restart-delay")
		}
		delay, err := flags.GetString("restart-backoff-base")
		if err != nil {
			return err
		}

		delayDuration, err := time.ParseDuration(delay)
		if err != nil {
			return err
		}

		if spec.Task.Restart.Backoff == nil {
			spec.Task.Restart.Backoff = &api.BackoffPolicy{}
		}

		spec.Task.Restart.Backoff.Base = gogotypes.DurationProto(delayDuration)
	}

	if flags.Changed("restart-backoff-factor") {
		if restartDelay {
			return errors.New("restart-backoff-factor is not compatible with restart-delay")
		}
		delay, err := flags.GetString("restart-backoff-factor")
		if err != nil {
			return err
		}

		delayDuration, err := time.ParseDuration(delay)
		if err != nil {
			return err
		}

		if spec.Task.Restart.Backoff == nil {
			spec.Task.Restart.Backoff = &api.BackoffPolicy{}
		}

		spec.Task.Restart.Backoff.Factor = gogotypes.DurationProto(delayDuration)
	}

	if flags.Changed("restart-backoff-max") {
		if restartDelay {
			return errors.New("restart-backoff-max is not compatible with restart-delay")
		}
		delay, err := flags.GetString("restart-backoff-max")
		if err != nil {
			return err
		}

		delayDuration, err := time.ParseDuration(delay)
		if err != nil {
			return err
		}

		if spec.Task.Restart.Backoff == nil {
			spec.Task.Restart.Backoff = &api.BackoffPolicy{}
		}

		spec.Task.Restart.Backoff.Max = gogotypes.DurationProto(delayDuration)
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

		spec.Task.Restart.Window = gogotypes.DurationProto(windowDelay)
	}

	return nil
}
