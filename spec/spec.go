package spec

import (
	"io/ioutil"

	yaml "github.com/cloudfoundry-incubator/candiedyaml"
	"github.com/docker/swarm-v2/api"
)

// Spec is a human representation of the API spec.
type Spec struct {
	Version  int                       `yaml:"version,omitempty"`
	Services map[string]*ServiceConfig `yaml:"services,omitempty"`
}

// ReadFrom creates a Spec from a file located at `path`.
func ReadFrom(path string) (*Spec, error) {
	s := &Spec{}

	data, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, err
	}

	if err := yaml.Unmarshal(data, s); err != nil {
		return nil, err
	}

	return s, s.validate()
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
		jobSpecs = append(jobSpecs, service.JobSpec())
	}
	return jobSpecs
}
