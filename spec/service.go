package spec

import (
	"fmt"
	"io"

	yaml "github.com/cloudfoundry-incubator/candiedyaml"
	specspb "github.com/docker/swarm-v2/pb/docker/cluster/specs"
	typespb "github.com/docker/swarm-v2/pb/docker/cluster/types"
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

// ToProto converts a ServiceConfig to a JobSpec.
func (s *ServiceConfig) ToProto() *specspb.JobSpec {
	spec := &specspb.JobSpec{
		Meta: specspb.Meta{
			Name:   s.Name,
			Labels: make(map[string]string),
		},
		Template: &specspb.TaskSpec{
			Runtime: &specspb.TaskSpec_Container{
				Container: &typespb.Container{
					Resources: s.Resources.ToProto(),
					Image: &typespb.Image{
						Reference: s.Image,
					},
					Env:     s.Env,
					Command: s.Command,
					Args:    s.Args,
				},
			},
		},
		Orchestration: &specspb.JobSpec_Service{
			Service: &specspb.JobSpec_ServiceJob{
				Instances: s.Instances,
			},
		},
	}

	return spec
}

// FromProto converts a JobSpec to a ServiceConfig.
func (s *ServiceConfig) FromProto(jobSpec *specspb.JobSpec) {
	*s = ServiceConfig{
		Name:      jobSpec.Meta.Name,
		Instances: jobSpec.GetService().Instances,
		ContainerConfig: ContainerConfig{
			Image:   jobSpec.Template.GetContainer().Image.Reference,
			Env:     jobSpec.Template.GetContainer().Env,
			Args:    jobSpec.Template.GetContainer().Args,
			Command: jobSpec.Template.GetContainer().Command,
		},
	}
	if jobSpec.Template.GetContainer().Resources != nil {
		s.Resources = &ResourceRequirements{}
		s.Resources.FromProto(jobSpec.Template.GetContainer().Resources)
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
