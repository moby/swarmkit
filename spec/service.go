package spec

import (
	"fmt"
	"io"
	"strings"
	"time"

	yaml "github.com/cloudfoundry-incubator/candiedyaml"
	"github.com/docker/swarm-v2/api"
	"github.com/pmezard/go-difflib/difflib"
)

const defaultStopGracePeriod = 60 * time.Second

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

	// Networks specifies all the networks that this service is attached to.
	Networks []string `yaml:"networks,omitempty"`

	// Ports specifies port mappings.
	Ports []PortConfig `yaml:"ports,omitempty"`

	// Mounts describe how volumes should be mounted in the container
	Mounts Mounts `yaml:"mounts,omitempty"`

	// StopGracePeriod is the amount of time to wait for the container
	// to terminate before forcefully killing it.
	StopGracePeriod string `yaml:"stopgraceperiod,omitempty"`
}

// PortConfig is a human representation of the PortConfiguration
type PortConfig struct {
	Name     string `yaml:"name,omitempty"`
	Protocol string `yaml:"protocol,omitempty"`
	Port     uint32 `yaml:"port,omitempty"`
	HostPort uint32 `yaml:"host_port,omitempty"`
}

// ServiceConfig is a human representation of the Service
type ServiceConfig struct {
	ContainerConfig

	Name      string  `yaml:"name,omitempty"`
	Instances *uint64 `yaml:"instances,omitempty"`
	Mode      string  `yaml:"mode,omitempty"`

	Restart *RestartConfiguration `yaml:"restart,omitempty"`
	Update  *UpdateConfiguration  `yaml:"update,omitempty"`
}

// Validate checks the validity of the ServiceConfig.
func (s *ServiceConfig) Validate() error {
	if s.Name == "" {
		return fmt.Errorf("name is mandatory")
	}
	if s.Image == "" {
		return fmt.Errorf("image is mandatory in %s", s.Name)
	}

	switch s.Mode {
	case "", "running":
	case "batch", "fill":
		if s.Instances != nil {
			return fmt.Errorf("instances is not allowed in %s services", s.Mode)
		}
	default:
		return fmt.Errorf("unrecognized mode %s", s.Mode)
	}

	if s.StopGracePeriod != "" {
		_, err := time.ParseDuration(s.StopGracePeriod)
		if err != nil {
			return err
		}
	}

	if s.Resources != nil {
		if err := s.Resources.Validate(); err != nil {
			return err
		}
	}
	if s.Update != nil {
		if err := s.Update.Validate(); err != nil {
			return err
		}
	}
	if s.Restart != nil {
		if err := s.Restart.Validate(); err != nil {
			return err
		}
	}

	if err := s.Mounts.Validate(); err != nil {
		return err
	}

	return nil
}

// Reset resets the service config to its defaults.
func (s *ServiceConfig) Reset() {
	*s = ServiceConfig{}
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
		Annotations: api.Annotations{
			Name: s.Name,
		},

		RuntimeSpec: &api.ServiceSpec_Container{
			Container: &api.ContainerSpec{
				Resources: s.Resources.ToProto(),
				Mounts:    s.Mounts.ToProto(),
				Image: api.Image{
					Reference: s.Image,
				},
				Env:     s.Env,
				Command: s.Command,
				Args:    s.Args,
			},
		},

		Update:  s.Update.ToProto(),
		Restart: s.Restart.ToProto(),
	}

	if len(s.Ports) != 0 {
		ports := []*api.PortConfig{}
		for _, portConfig := range s.Ports {
			ports = append(ports, &api.PortConfig{
				Name:     portConfig.Name,
				Protocol: api.PortConfig_Protocol(api.PortConfig_Protocol_value[strings.ToUpper(portConfig.Protocol)]),
				Port:     portConfig.Port,
				HostPort: portConfig.HostPort,
			})
		}

		spec.GetContainer().ExposedPorts = ports
	}

	if len(s.Networks) != 0 {
		networks := make([]*api.ContainerSpec_NetworkAttachment, 0, len(s.Networks))
		for _, net := range s.Networks {
			networks = append(networks, &api.ContainerSpec_NetworkAttachment{
				Reference: &api.ContainerSpec_NetworkAttachment_NetworkID{
					NetworkID: net,
				},
			})
		}

		spec.GetContainer().Networks = networks
	}

	switch s.Mode {
	case "", "running":
		spec.Mode = api.ServiceModeRunning
		// Default to 1 instance.
		spec.Instances = 1
		if s.Instances != nil {
			spec.Instances = *s.Instances
		}
	case "batch":
		spec.Mode = api.ServiceModeBatch
	case "fill":
		spec.Mode = api.ServiceModeFill
	}

	if s.StopGracePeriod == "" {
		spec.GetContainer().StopGracePeriod = defaultStopGracePeriod
	} else {
		spec.GetContainer().StopGracePeriod, _ = time.ParseDuration(s.StopGracePeriod)
	}

	return spec
}

// FromProto converts a ServiceSpec to a ServiceConfig.
func (s *ServiceConfig) FromProto(serviceSpec *api.ServiceSpec) {
	*s = ServiceConfig{
		Name:      serviceSpec.Annotations.Name,
		Instances: &serviceSpec.Instances,
		ContainerConfig: ContainerConfig{
			Image:   serviceSpec.GetContainer().Image.Reference,
			Env:     serviceSpec.GetContainer().Env,
			Args:    serviceSpec.GetContainer().Args,
			Command: serviceSpec.GetContainer().Command,
		},
	}
	if serviceSpec.GetContainer().Resources != nil {
		s.Resources = &ResourceRequirements{}
		s.Resources.FromProto(serviceSpec.GetContainer().Resources)
	}

	if serviceSpec.GetContainer().Mounts != nil {
		apiMounts := serviceSpec.GetContainer().Mounts
		s.Mounts = make(Mounts, len(apiMounts))
		s.Mounts.FromProto(apiMounts)
	}

	if serviceSpec.Endpoint != nil {
		for _, port := range serviceSpec.GetContainer().ExposedPorts {
			s.Ports = append(s.Ports, PortConfig{
				Name:     port.Name,
				Protocol: strings.ToLower(port.Protocol.String()),
				Port:     port.Port,
				HostPort: port.HostPort,
			})
		}
	}

	if serviceSpec.GetContainer().Networks != nil {
		for _, net := range serviceSpec.GetContainer().Networks {
			s.Networks = append(s.Networks, net.GetNetworkID())
		}
	}

	switch serviceSpec.Mode {
	case api.ServiceModeRunning:
		s.Mode = "running"
	case api.ServiceModeFill:
		s.Mode = "fill"
	case api.ServiceModeBatch:
		s.Mode = "batch"
	}

	s.StopGracePeriod = serviceSpec.GetContainer().StopGracePeriod.String()

	if serviceSpec.Update != nil {
		s.Update = &UpdateConfiguration{}
		s.Update.FromProto(serviceSpec.Update)
	}

	if serviceSpec.Restart != nil {
		s.Restart = &RestartConfiguration{}
		s.Restart.FromProto(serviceSpec.Restart)
	}
}

// Diff returns a diff between two ServiceConfigs.
func (s *ServiceConfig) Diff(context int, fromFile, toFile string, other *ServiceConfig) (string, error) {
	// Marshal back and forth to make sure we run with the same defaults.
	from := &ServiceConfig{}
	from.FromProto(other.ToProto())

	to := &ServiceConfig{}
	to.FromProto(s.ToProto())

	fromYml, err := yaml.Marshal(from)
	if err != nil {
		return "", err
	}

	toYml, err := yaml.Marshal(to)
	if err != nil {
		return "", err
	}

	diff := difflib.UnifiedDiff{
		A:        difflib.SplitLines(string(fromYml)),
		FromFile: fromFile,
		B:        difflib.SplitLines(string(toYml)),
		ToFile:   toFile,
		Context:  context,
	}

	return difflib.GetUnifiedDiffString(diff)
}
