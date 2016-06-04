package spec

import (
	"fmt"
	"strings"

	"github.com/docker/swarm-v2/api"
)

// Driver defines the structure of a driver definitions, used with volume
// templates and networks.
type Driver struct {
	Name string   `yaml:"name,omitempty"`
	Opts []string `yaml:"opts,omitempty"`
}

// VolumeTemplate is a human representation of plugin volumes mounted into the Task
type VolumeTemplate struct {
	Name string `yaml:"name,omitempty"`

	Driver Driver `yaml:"driver,omitempty"`

	// TODO(stevvooe): Allow specification of labels with yaml format.
}

// Mount is a human representation of VolumeSpec & contains volumes to be mounted into the Task
type Mount struct {
	// Location for mount inside the container
	Target string `yaml:"target,omitempty"`

	// Source directory to be mounted
	Source string `yaml:"source,omitempty"`

	// Supported types are: bind, ephemeral, volume
	Type string `yaml:"type,omitempty"`

	// Specify mount as writable
	Writable bool `yaml:"writable,omitempty"`

	// Supported values: [r]private, [r]shared, [r]slave
	// `bind` type only
	Propagation string `yaml:"propagation,omitempty"`

	// Set MCS label for sharing mode.
	// `bind` type only
	MCSAccessMode string `yaml:"mcsaccessmode,omitempty"`

	// Populate volume with data from the target
	Populate bool `yaml:"populate,omitempty"`

	// VolumeTemplate describes how plugin volumes are mounted in the container
	Template *VolumeTemplate `yaml:"template,omitempty"`
}

// Mounts - defined to add To/FromProto methods
type Mounts []Mount

// Validate checks the validity of a Mount
func (vm *Mount) Validate() error {
	// Assume everything is alright if mounts are not specified.
	if vm == nil {
		return nil
	}

	if vm.Target == "" {
		return fmt.Errorf("target is required")
	}

	switch strings.ToLower(vm.Type) {
	case "bind":
		if vm.Source == "" {
			return fmt.Errorf("for volume mount type 'bind', source is required")
		}
		switch vm.Propagation {
		case "private", "rprivate", "shared", "rshared", "slave", "rslave", "":
		default:
			return fmt.Errorf("for volume type 'bind', propagation must be one of [r]private, r[shared], r[slave]")
		}
		switch vm.MCSAccessMode {
		case "shared", "private", "none", "":
		default:
			return fmt.Errorf("for volume type 'bind', mcsaccessmode must be one of shared, private, or none")
		}
		if vm.Populate {
			return fmt.Errorf("for volume mount type 'bind', populate must not be set")
		}
		if vm.Template != nil {
			return fmt.Errorf("for volume mount type 'bind', template cannot be specified")
		}
	case "volume":
		if vm.Source != "" {
			return fmt.Errorf("for volume mount type 'volume', source cannot be specified")
		}
		if vm.Propagation != "" {
			return fmt.Errorf("for volume mount type 'volume', propagation cannot be specified")
		}
		if vm.MCSAccessMode != "" {
			return fmt.Errorf("for volume mount type 'volume', mcsaccessshared cannot be specified")
		}
		if vm.Template != nil { // template is not required
			if vm.Template.Name == "" {
				return fmt.Errorf("for volume mount type 'volume', template requires a name")
			}
		}
	default:
		return fmt.Errorf("invalid volume mount type: %s", vm.Type)
	}

	return nil
}

// Validate checks the validity of Mounts
func (vms Mounts) Validate() error {
	// Assume everything is alright if mounts are not specified.
	if vms == nil {
		return nil
	}
	for i := range vms {
		err := vms[i].Validate()
		if err != nil {
			return err
		}
	}
	return nil
}

// ToProto converts native Mounts into protos.
func (vms Mounts) ToProto() []*api.Mount {
	if vms == nil {
		return nil
	}

	apiVM := make([]*api.Mount, len(vms))

	// No error checking here - `Validate` must have been called before.
	for i, vm := range vms {
		apiVM[i] = vm.ToProto()
	}

	return apiVM
}

// ToProto converts native Mount into proto.
func (vm *Mount) ToProto() *api.Mount {
	if vm == nil {
		return nil
	}

	apiVM := &api.Mount{}
	apiVM.Target = vm.Target
	apiVM.Source = vm.Source
	apiVM.Writable = vm.Writable
	apiVM.Populate = vm.Populate
	switch strings.ToLower(vm.Propagation) {
	case "private":
		apiVM.Propagation = api.MountPropagationPrivate
	case "rprivate":
		apiVM.Propagation = api.MountPropagationRPrivate
	case "shared":
		apiVM.Propagation = api.MountPropagationShared
	case "rshared":
		apiVM.Propagation = api.MountPropagationSlave
	case "slave":
		apiVM.Propagation = api.MountPropagationSlave
	case "rslave":
		apiVM.Propagation = api.MountPropagationRSlave
	}

	switch strings.ToLower(vm.MCSAccessMode) {
	case "shared":
		apiVM.Mcsaccessmode = api.MountMCSAccessModeShared
	case "private":
		apiVM.Mcsaccessmode = api.MountMCSAccessModePrivate
	case "none", "":
		apiVM.Mcsaccessmode = api.MountMCSAccessModeNone
	}

	switch strings.ToLower(vm.Type) {
	case "bind":
		apiVM.Type = api.MountTypeBind
	case "volume":
		apiVM.Type = api.MountTypeVolume
	}

	if vm.Template != nil {

		var driver *api.Driver
		if vm.Template.Driver.Name != "" {
			opts, _ := vm.parseDriverOptions(vm.Template.Driver.Opts)
			driver = &api.Driver{
				Name:    vm.Template.Driver.Name,
				Options: opts,
			}
		}

		apiVM.Template = &api.VolumeTemplate{
			Annotations: api.Annotations{
				Name: vm.Template.Name,
				// TODO(stevvooe): Add labels support in yaml format.
				// Labels: vm.Template.Labels,
			},
			DriverConfig: driver,
		}
	}

	return apiVM
}

// FromProto converts proto array of Mounts back into native types.
func (vms Mounts) FromProto(p []*api.Mount) {
	if p == nil {
		return
	}

	for i, apivm := range p {
		vm := Mount{}
		vm.FromProto(apivm)
		vms[i] = vm
	}
}

// FromProto converts proto Mount back into native types.
func (vm *Mount) FromProto(apivm *api.Mount) {
	if vm == nil {
		return
	}

	vm.Target = apivm.Target
	vm.Source = apivm.Source
	vm.Writable = apivm.Writable
	vm.Populate = apivm.Populate

	switch apivm.Propagation {
	case api.MountPropagationPrivate:
		vm.Propagation = "private"
	case api.MountPropagationRPrivate:
		vm.Propagation = "rprivate"
	case api.MountPropagationShared:
		vm.Propagation = "shared"
	case api.MountPropagationRShared:
		vm.Propagation = "rshared"
	case api.MountPropagationSlave:
		vm.Propagation = "slave"
	case api.MountPropagationRSlave:
		vm.Propagation = "rslave"
	}

	switch apivm.Mcsaccessmode {
	case api.MountMCSAccessModeShared:
		vm.MCSAccessMode = "shared"
	case api.MountMCSAccessModePrivate:
		vm.MCSAccessMode = "private"
	}

	switch apivm.Type {
	case api.MountTypeBind:
		vm.Type = "bind"
	case api.MountTypeVolume:
		vm.Type = "volume"
	}

	if apivm.Template != nil { // only send it if defined.
		opts := vm.convertDriverOptionsToArray(apivm.Template.DriverConfig.Options)

		vm.Template = new(VolumeTemplate)
		vm.Template.Name = apivm.Template.Annotations.Name
		vm.Template.Driver.Name = apivm.Template.DriverConfig.Name
		vm.Template.Driver.Opts = opts
	}
}

func (vm *Mount) parseDriverOptions(opts []string) (map[string]string, error) {
	if len(opts) == 0 {
		return nil, nil
	}

	parsedOptions := map[string]string{}
	for _, opt := range opts {
		optPair := strings.Split(opt, "=")
		if len(optPair) != 2 {
			return nil, fmt.Errorf("Malformed opts: %s", opt)
		}
		parsedOptions[optPair[0]] = optPair[1]
	}

	return parsedOptions, nil
}

func (vm *Mount) convertDriverOptionsToArray(driverOpts map[string]string) []string {
	var r []string
	for k, v := range driverOpts {
		r = append(r, fmt.Sprintf("%s=%s", k, v))
	}
	return r
}
