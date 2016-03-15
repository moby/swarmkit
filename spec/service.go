package spec

import (
	"fmt"
	"io"

	yaml "github.com/cloudfoundry-incubator/candiedyaml"
	"github.com/docker/swarm-v2/api"
	"github.com/pmezard/go-difflib/difflib"
)

// ContainerConfig is a human representation of the ContainerSpec
type ContainerConfig struct {
	Image string `yaml:"image,omitempty"`
}

// ServiceConfig is a human representation of the ServiceJob
type ServiceConfig struct {
	ContainerConfig

	Name      string `yaml:"name,omitempty"`
	Instances int64  `yaml:"instances,omitempty"`
}

// Validate checks the validity of the ServiceConfig.
func (s *ServiceConfig) Validate() error {
	if s.Name == "" {
		return fmt.Errorf("name is mandatory")
	}
	if s.Image == "" {
		return fmt.Errorf("image is mandatory in %s", s.Name)
	}
	return nil
}

// Parse creates a ServiceConfig from a io.Reader.
func (s *ServiceConfig) Parse(r io.Reader) error {
	s.Instances = 1

	if err := yaml.NewDecoder(r).Decode(s); err != nil {
		return err
	}

	return s.Validate()
}

// JobSpec converts a ServiceConfig to a JobSpec.
func (s *ServiceConfig) JobSpec() *api.JobSpec {
	return &api.JobSpec{
		Meta: api.Meta{
			Name: s.Name,
		},
		Template: &api.TaskSpec{
			Runtime: &api.TaskSpec_Container{
				Container: &api.ContainerSpec{
					Image: &api.ImageSpec{
						Reference: s.Image,
					},
				},
			},
		},
		Orchestration: &api.JobSpec_Service{
			Service: &api.JobSpec_ServiceJob{
				Instances: s.Instances,
			},
		},
	}
}

// FromJobSpec converts a JobSpec to a ServiceConfig.
func (s *ServiceConfig) FromJobSpec(jobSpec *api.JobSpec) {
	s.Image = jobSpec.Template.GetContainer().Image.Reference
	s.Name = jobSpec.Meta.Name
	s.Instances = jobSpec.GetService().Instances
}

// Diff returns a diff between two ServiceConfigs.
func (s *ServiceConfig) Diff(other *ServiceConfig) (string, error) {
	local, err := yaml.Marshal(s)
	if err != nil {
		return "", err
	}
	remote, err := yaml.Marshal(other)
	if err != nil {
		return "", err
	}

	diff := difflib.UnifiedDiff{
		A:        difflib.SplitLines(string(remote)),
		B:        difflib.SplitLines(string(local)),
		FromFile: "remote",
		ToFile:   "local",
	}

	return difflib.GetUnifiedDiffString(diff)
}
