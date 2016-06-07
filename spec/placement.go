package spec

import "github.com/docker/swarmkit/api"

// PlacementConfig represents task placement configs
type PlacementConfig struct {
	Constraints []string `yaml:"constraints,omitempty"`
}

// Validate checks the validity of the configuration
func (p *PlacementConfig) Validate() error {
	if p == nil {
		return nil
	}
	if _, err := ParseExprs(p.Constraints); err != nil {
		return err
	}
	return nil
}

// ToProto converts native PlacementConfig to protos.
func (p *PlacementConfig) ToProto() *api.Placement {
	if p == nil {
		return nil
	}
	r := &api.Placement{
		Constraints: p.Constraints,
	}
	return r
}

// FromProto converts proto Placement back to PlacementConfig
func (p *PlacementConfig) FromProto(r *api.Placement) {
	if r == nil {
		return
	}

	p.Constraints = r.Constraints
}
