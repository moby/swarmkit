package spec

import (
	"fmt"
	"strings"

	"github.com/docker/swarm-v2/api"
)

// VolumeConfig is a human representation of the Volumes that use drivers
// The reason DriverOpts is an array rather than a map is that it results in a
// compact representation in YAML
type VolumeConfig struct {
	Name       string   `toml:"name,omitempty"`
	Driver     string   `toml:"driver,omitempty"`
	DriverOpts []string `toml:"opts,omitempty"`
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

	if _, err := vc.parseDriverOptions(vc.DriverOpts); err != nil {
		return err
	}

	return nil
}

// ToProto converts native VolumeSpecs into protos
func (vc *VolumeConfig) ToProto() *api.VolumeSpec {
	if vc == nil {
		return nil
	}

	opts, _ := vc.parseDriverOptions(vc.DriverOpts)
	apiVol := api.VolumeSpec{
		Annotations: api.Annotations{
			Name:   vc.Name,
			Labels: make(map[string]string),
		},
		DriverConfiguration: &api.Driver{
			Name:    vc.Driver,
			Options: opts,
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
	vc.DriverOpts = vc.convertDriverOptionsToArray(apiVol.DriverConfiguration.Options)
}

func (vc *VolumeConfig) parseDriverOptions(opts []string) (map[string]string, error) {
	parsedOptions := map[string]string{}
	for _, opt := range opts {
		optPair := strings.Split(opt, "=")
		if len(optPair) != 2 {
			return nil, fmt.Errorf("Malformed opts: %s", opt)
		}
		parsedOptions[optPair[0]] = optPair[1]
	}

	return parsedOptions, nil
}

func (vc *VolumeConfig) convertDriverOptionsToArray(driverOpts map[string]string) []string {
	var r []string
	for k, v := range driverOpts {
		r = append(r, fmt.Sprintf("%s=%s", k, v))
	}
	return r
}
