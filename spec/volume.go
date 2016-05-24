package spec

import (
	"fmt"

	"github.com/docker/swarm-v2/api"
)

// VolumeConfig is a human representation of the Volumes that use drivers
// The reason DriverOpts is an array rather than a map is that it results in a
// compact representation in YAML
type VolumeConfig struct {
	Name       string   `yaml:"name,omitempty"`
	Driver     string   `yaml:"driver,omitempty"`
	DriverOpts []string `yaml:"opts,omitempty"`
}

// Validate checks the validity of a VolumeConfig
func (vc *VolumeConfig) Validate() error {
	// Assume everything is alright if volumes are not specified.
	if vc == nil {
		return nil
	}

	if vc.Name == "" || vc.Driver == "" {
		return fmt.Errorf("In a volume specification, the name and driver are required")
	}

	return nil
}

// ToProto converts native VolumeSpecs into protos
func (vc *VolumeConfig) ToProto() *api.VolumeSpec {
	if vc == nil {
		return nil
	}

	apiVol := api.VolumeSpec{
		Annotations: api.Annotations{
			Name: vc.Name,
		},
		DriverConfiguration: &api.Driver{
			Name:    vc.Driver,
			Options: vc.DriverOpts,
		},
	}

	return &apiVol
}

// FromProto converts proto VolumeSpecs back into native types.
func (vc *VolumeConfig) FromProto(apiVol *api.VolumeSpec) {
	if vc == nil {
		return
	}

	vc.Name = apiVol.Annotations.Name
	vc.Driver = apiVol.DriverConfiguration.Name
	vc.DriverOpts = apiVol.DriverConfiguration.Options
}
