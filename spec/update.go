package spec

import (
	"time"

	"github.com/docker/swarmkit/api"
	"github.com/docker/swarmkit/protobuf/ptypes"
)

// UpdateConfiguration controls the rate and policy of updates.
type UpdateConfiguration struct {
	Parallelism uint64 `yaml:"parallelism,omitempty"`
	Delay       string `yaml:"delay,omitempty"`
}

// Validate checks the validity of the strategy.
func (u *UpdateConfiguration) Validate() error {
	if u == nil {
		return nil
	}
	if u.Delay != "" {
		if _, err := time.ParseDuration(u.Delay); err != nil {
			return err
		}
	}
	return nil
}

// ToProto converts native UpdateConfiguration into protos.
func (u *UpdateConfiguration) ToProto() *api.UpdateConfig {
	if u == nil {
		return nil
	}
	p := &api.UpdateConfig{
		Parallelism: u.Parallelism,
	}
	if u.Delay != "" {
		delay, _ := time.ParseDuration(u.Delay)
		p.Delay = *ptypes.DurationProto(delay)
	}
	return p
}

// FromProto converts proto UpdateConfiguration back into native types.
func (u *UpdateConfiguration) FromProto(p *api.UpdateConfig) {
	if p == nil {
		return
	}

	delay, _ := ptypes.Duration(&p.Delay)

	*u = UpdateConfiguration{
		Parallelism: p.Parallelism,
		Delay:       delay.String(),
	}
}
