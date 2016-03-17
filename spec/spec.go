package spec

import (
	"fmt"
	"io"

	yaml "github.com/cloudfoundry-incubator/candiedyaml"
	"github.com/docker/swarm-v2/api"
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

// JobSpecs returns all the JobSpecs in the Spec.
func (s *Spec) JobSpecs() []*api.JobSpec {
	jobSpecs := []*api.JobSpec{}
	for _, service := range s.Services {
		jobSpec := service.JobSpec()
		jobSpec.Meta.Labels["namespace"] = s.Namespace
		jobSpecs = append(jobSpecs, jobSpec)
	}
	return jobSpecs
}

// FromJobSpecs converts Jobs to a Spec.
func (s *Spec) FromJobSpecs(jobspecs []*api.JobSpec) {
	for _, j := range jobspecs {
		service := &ServiceConfig{}
		service.FromJobSpec(j)
		s.Services[j.Meta.Name] = service
	}
}

// Diff returns a diff between two Specs.
func (s *Spec) Diff(context int, fromFile, toFile string, other *Spec) (string, error) {
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
