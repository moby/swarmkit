package spec

import (
	"fmt"
	"io/ioutil"

	yaml "github.com/cloudfoundry-incubator/candiedyaml"
	"github.com/docker/swarm-v2/api"
)

// ServiceConfig is a human representation of the ServiceJob
type ServiceConfig struct {
	ContainerConfig

	Instances int64 `yaml:"instances,omitempty"`
}

// ContainerConfig is a human representation of the ContainerSpec
type ContainerConfig struct {
	Image string `yaml:"image,omitempty"`
}

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
		if service.Image == "" {
			return fmt.Errorf("image is mandatory in %s", name)
		}
	}
	return nil
}

// JobSpecs returns all the JobSpecs in the Spec.
func (s *Spec) JobSpecs() []*api.JobSpec {
	jobSpecs := []*api.JobSpec{}
	for name, service := range s.Services {
		// TODO(vieux): what if we want to pass 0 in the file?
		if service.Instances == 0 {
			service.Instances = 1
		}
		jobSpecs = append(jobSpecs, &api.JobSpec{
			Meta: api.Meta{
				Name: name,
			},
			Template: &api.TaskSpec{
				Runtime: &api.TaskSpec_Container{
					Container: &api.ContainerSpec{
						Image: &api.ImageSpec{
							Reference: service.Image,
						},
					},
				},
			},
			Orchestration: &api.JobSpec_Service{
				Service: &api.JobSpec_ServiceJob{
					Instances: service.Instances,
				},
			},
		})
	}
	return jobSpecs
}
