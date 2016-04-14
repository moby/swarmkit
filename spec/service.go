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

	Resources *ResourceRequirements `yaml:"resources,omitempty"`
}

// ServiceConfig is a human representation of the Service
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
	if err := s.Resources.Validate(); err != nil {
		return err
	}
	return nil
}

// Reset resets the service config to its defaults.
func (s *ServiceConfig) Reset() {
	*s = ServiceConfig{
		Instances: 1,
	}
}

// Read reads a ServiceConfig from an io.Reader.
func (s *ServiceConfig) Read(r io.Reader) error {
	s.Reset()

	if err := yaml.NewDecoder(r).Decode(s); err != nil {
		return err
	}

	return s.Validate()
}

// Write writes a ServiceConfig to an io.Reader.
func (s *ServiceConfig) Write(w io.Writer) error {
	return yaml.NewEncoder(w).Encode(s)
}

// ToProto converts a ServiceConfig to a ServiceSpec.
func (s *ServiceConfig) ToProto() *api.ServiceSpec {
	spec := &api.ServiceSpec{
		Meta: api.Meta{
			Name:   s.Name,
			Labels: make(map[string]string),
		},
		Template: &api.TaskSpec{
			Runtime: &api.TaskSpec_Container{
				Container: &api.Container{
					Resources: s.Resources.ToProto(),
					Image: &api.Image{
						Reference: s.Image,
					},
					Env:     s.Env,
					Command: s.Command,
					Args:    s.Args,
				},
			},
		},
		Instances: s.Instances,
	}

	return spec
}

// FromProto converts a ServiceSpec to a ServiceConfig.
func (s *ServiceConfig) FromProto(serviceSpec *api.ServiceSpec) {
	*s = ServiceConfig{
		Name:      serviceSpec.Meta.Name,
		Instances: serviceSpec.Instances,
		ContainerConfig: ContainerConfig{
			Image:   serviceSpec.Template.GetContainer().Image.Reference,
			Env:     serviceSpec.Template.GetContainer().Env,
			Args:    serviceSpec.Template.GetContainer().Args,
			Command: serviceSpec.Template.GetContainer().Command,
		},
	}
	if serviceSpec.Template.GetContainer().Resources != nil {
		s.Resources = &ResourceRequirements{}
		s.Resources.FromProto(serviceSpec.Template.GetContainer().Resources)
	}
}

// Diff returns a diff between two ServiceConfigs.
func (s *ServiceConfig) Diff(context int, fromFile, toFile string, other *ServiceConfig) (string, error) {
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
