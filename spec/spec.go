package spec

import (
	"bytes"
	"io"

	"github.com/BurntSushi/toml"
	"github.com/docker/swarm-v2/api"
	"github.com/pmezard/go-difflib/difflib"
)

// Spec is a human representation of the API spec.
type Spec struct {
	Version   string                    `toml:"version,omitempty"`
	Namespace string                    `toml:"namespace,omitempty"`
	Services  map[string]*ServiceConfig `toml:"services,omitempty"`
	Volumes   map[string]*VolumeConfig  `toml:"volumes,omitempty"`
}

// Read reads a Spec from an io.Reader.
func (s *Spec) Read(r io.Reader) error {
	s.Reset()
	if _, err := toml.DecodeReader(r, s); err != nil {
		return err
	}
	if err := s.validate(); err != nil {
		return err
	}

	if s.Version == 2 {
		fmt.Println("WARNING: v2 format is only partially supported, please update to v3")
	}
	return nil
}

// Reset resets the service config to its defaults.
func (s *Spec) Reset() {
	*s = Spec{}
}

func (s *Spec) validate() error {
	for name, service := range s.Services {
		service.Name = name
		if err := service.Validate(); err != nil {
			return err
		}
	}
	for name, volume := range s.Volumes {
		volume.Name = name
		if err := volume.Validate(); err != nil {
			return err
		}
	}
	return nil
}

// ServiceSpecs returns all the ServiceSpecs in the Spec.
func (s *Spec) ServiceSpecs() []*api.ServiceSpec {
	serviceSpecs := []*api.ServiceSpec{}
	for _, service := range s.Services {
		serviceSpec := service.ToProto()
		serviceSpec.Annotations.Labels["namespace"] = s.Namespace
		serviceSpecs = append(serviceSpecs, serviceSpec)
	}
	return serviceSpecs
}

// FromServiceSpecs converts Services to a Spec.
func (s *Spec) FromServiceSpecs(servicespecs []*api.ServiceSpec) {
	for _, j := range servicespecs {
		service := &ServiceConfig{}
		service.FromProto(j)
		s.Services[j.Annotations.Name] = service
	}
}

// VolumeSpecs returns all the VolumeSpecs in the Spec.
func (s *Spec) VolumeSpecs() []*api.VolumeSpec {
	volumeSpecs := []*api.VolumeSpec{}
	for _, volume := range s.Volumes {
		volumeSpec := volume.ToProto()
		volumeSpec.Annotations.Labels["namespace"] = s.Namespace
		volumeSpecs = append(volumeSpecs, volumeSpec)
	}
	return volumeSpecs
}

// FromVolumeSpecs converts Volumes to a Spec.
func (s *Spec) FromVolumeSpecs(volumespecs []*api.VolumeSpec) {
	for _, j := range volumespecs {
		volume := &VolumeConfig{}
		volume.FromProto(j)
		s.Volumes[j.Annotations.Name] = volume
	}
}

func encodeString(val interface{}) (string, error) {
	var buf bytes.Buffer
	enc := toml.NewEncoder(&buf)
	err := enc.Encode(val)
	if err != nil {
		return "", err
	}
	return buf.String(), nil
}

// Diff returns a diff between two Specs.
func (s *Spec) Diff(context int, fromFile, toFile string, other *Spec) (string, error) {
	// Force marshal/unmarshal.
	other.FromServiceSpecs(other.ServiceSpecs())
	s.FromServiceSpecs(s.ServiceSpecs())

	from, err := encodeString(s)
	if err != nil {
		return "", err
	}

	to, err := encodeString(other)
	if err != nil {
		return "", err
	}

	diff := difflib.UnifiedDiff{
		A:        difflib.SplitLines(string(from)),
		FromFile: fromFile,
		B:        difflib.SplitLines(string(to)),
		ToFile:   toFile,
		Context:  context,
	}

	return difflib.GetUnifiedDiffString(diff)
}
