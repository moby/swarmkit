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

	// Command to run the the container. The first element is a path to the
	// executable and the following elements are treated as arguments.
	//
	// If command is empty, execution will fall back to the image's entrypoint.
	Command []string `yaml:"command,omitempty"`

	// Args specifies arguments provided to the image's entrypoint.
	// Ignored if command is specified.
	Args []string `yaml:"args,omitempty"`

	// Env specifies the environment variables for the container in NAME=VALUE
	// format. These must be compliant with  [IEEE Std
	// 1003.1-2001](http://pubs.opengroup.org/onlinepubs/009695399/basedefs/xbd_chap08.html).
	Env []string `yaml:"env,omitempty"`
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
			Name:   s.Name,
			Labels: make(map[string]string),
		},
		Template: &api.TaskSpec{
			Runtime: &api.TaskSpec_Container{
				Container: &api.ContainerSpec{
					Image: &api.ImageSpec{
						Reference: s.Image,
					},
					Env:     s.Env,
					Command: s.Command,
					Args:    s.Args,
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
	s.Env = jobSpec.Template.GetContainer().Env
	s.Args = jobSpec.Template.GetContainer().Args
	s.Command = jobSpec.Template.GetContainer().Command
}

// Diff returns a diff between two ServiceConfigs.
func (s *ServiceConfig) Diff(fromFile, toFile string, other *ServiceConfig) (string, error) {
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
	}

	return difflib.GetUnifiedDiffString(diff)
}
