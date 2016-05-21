package spec

import (
	"time"

	"github.com/docker/swarm-v2/api"
)

// UpdateConfiguration controls the rate and policy of updates.
type UpdateConfiguration struct {
	Parallelism uint64 `toml:"parallelism,omitempty"`
	Delay       string `toml:"delay,omitempty"`
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
		p.Delay, _ = time.ParseDuration(u.Delay)
	}
	return p
}

// FromProto converts proto UpdateConfiguration back into native types.
func (u *UpdateConfiguration) FromProto(p *api.UpdateConfig) {
	if p == nil {
		return
	}

	*u = UpdateConfiguration{
		Parallelism: p.Parallelism,
		Delay:       p.Delay.String(),
	}
}
