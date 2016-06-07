package spec

import (
	"fmt"
	"time"

	"github.com/docker/swarmkit/api"
	"github.com/docker/swarmkit/protobuf/ptypes"
)

// RestartConfiguration controls the rate and policy of restarts.
type RestartConfiguration struct {
	On       string `yaml:"on,omitempty"`
	Delay    string `yaml:"delay,omitempty"`
	Attempts uint64 `yaml:"attempts,omitempty"`
	Window   string `yaml:"window,omitempty"`
}

// Validate checks the validity of the configuration.
func (u *RestartConfiguration) Validate() error {
	if u == nil {
		return nil
	}
	switch u.On {
	case "", "none", "failure", "any":
	default:
		return fmt.Errorf("unrecognized restarton value %s", u.On)
	}
	if u.Delay != "" {
		_, err := time.ParseDuration(u.Delay)
		if err != nil {
			return err
		}
	}
	if u.Window != "" {
		_, err := time.ParseDuration(u.Window)
		if err != nil {
			return err
		}
	}

	return nil
}

// ToProto converts native RestartConfiguration into protos.
func (u *RestartConfiguration) ToProto() *api.RestartPolicy {
	if u == nil {
		return nil
	}
	p := &api.RestartPolicy{
		MaxAttempts: u.Attempts,
	}

	switch u.On {
	case "none":
		p.Condition = api.RestartOnNone
	case "failure":
		p.Condition = api.RestartOnFailure
	case "", "any":
		p.Condition = api.RestartOnAny
	}

	if u.Delay != "" {
		delay, _ := time.ParseDuration(u.Delay)
		p.Delay = ptypes.DurationProto(delay)
	}

	if u.Window != "" {
		window, _ := time.ParseDuration(u.Window)
		p.Window = ptypes.DurationProto(window)
	}

	return p
}

// FromProto converts proto UpdateConfiguration back into native types.
func (u *RestartConfiguration) FromProto(p *api.RestartPolicy) {
	if p == nil {
		return
	}

	*u = RestartConfiguration{
		Attempts: p.MaxAttempts,
	}

	if p.Delay != nil {
		delay, _ := ptypes.Duration(p.Delay)
		u.Delay = delay.String()
	}

	if p.Window != nil {
		window, _ := ptypes.Duration(p.Window)
		u.Window = window.String()
	}

	switch p.Condition {
	case api.RestartOnNone:
		u.On = "none"
	case api.RestartOnFailure:
		u.On = "failure"
	case api.RestartOnAny:
		u.On = "any"
	}
}
