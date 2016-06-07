package spec

import (
	"fmt"
	"io"

	yaml "github.com/cloudfoundry-incubator/candiedyaml"
	"github.com/docker/swarmkit/api"
	"github.com/pmezard/go-difflib/difflib"
)

// Spec is a human representation of the API spec.
type Spec struct {
	Version   int                       `yaml:"version,omitempty"`
	Namespace string                    `yaml:"namespace,omitempty"`
	Services  map[string]*ServiceConfig `yaml:"services,omitempty"`
}

// Read reads a Spec from an io.Reader.
func (s *Spec) Read(r io.Reader) error {
	s.Reset()
	if err := yaml.NewDecoder(r).Decode(s); err != nil {
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

// Diff returns a diff between two Specs.
func (s *Spec) Diff(context int, fromFile, toFile string, other *Spec) (string, error) {
	// Force marshal/unmarshal.
	other.FromServiceSpecs(other.ServiceSpecs())
	s.FromServiceSpecs(s.ServiceSpecs())

	from, err := yaml.Marshal(other)
	if err != nil {
		return "", err
	}

	to, err := yaml.Marshal(s)
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
