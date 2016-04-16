package spec

import (
	"time"

	"github.com/docker/swarm-v2/api"
)

// UpdateStrategy controls the rate and policy of updates.
type UpdateStrategy struct {
	Parallelism uint64 `yaml:"parallelism,omitempty"`
	Delay       string `yaml:"delay,omitempty"`
}

// Validate checks the validity of the strategy.
func (u *UpdateStrategy) Validate() error {
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

// ToProto converts native UpdateStrategy into protos.
func (u *UpdateStrategy) ToProto() *api.UpdateStrategy {
	if u == nil {
		return nil
	}
	p := &api.UpdateStrategy{
		Parallelism: u.Parallelism,
	}
	if u.Delay != "" {
		p.Delay, _ = time.ParseDuration(u.Delay)
	}
	return p
}

// FromProto converts proto UpdateStrategy back into native types.
func (u *UpdateStrategy) FromProto(p *api.UpdateStrategy) {
	if p == nil {
		return
	}

	*u = UpdateStrategy{
		Parallelism: p.Parallelism,
		Delay:       p.Delay.String(),
	}
}
