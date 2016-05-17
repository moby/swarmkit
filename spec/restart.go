package spec

import (
	"fmt"
	"time"

	"github.com/docker/swarm-v2/api"
)

const defaultRestartDelay = 5 * time.Second

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

	if u.Delay == "" {
		p.Delay = defaultRestartDelay
	} else {
		p.Delay, _ = time.ParseDuration(u.Delay)
	}

	p.Window, _ = time.ParseDuration(u.Window)

	return p
}

// FromProto converts proto UpdateConfiguration back into native types.
func (u *RestartConfiguration) FromProto(p *api.RestartPolicy) {
	if p == nil {
		return
	}

	*u = RestartConfiguration{
		Delay:    p.Delay.String(),
		Attempts: p.MaxAttempts,
		Window:   p.Window.String(),
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
