package spec

import (
	"fmt"
	"strings"

	"github.com/docker/swarm-v2/api"
)

// Mount is a human representation of VolumeSpec & contains volumes to be mounted into the Task
type Mount struct {
	// Location for mount inside the container
	Target string `yaml:"target,omitempty"`

	// Source directory to be mounted
	Source string `yaml:"source,omitempty"`

	// Name of plugin volume
	VolumeName string `yaml:"name,omitempty"`

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
	case "ephemeral":
		if vm.Source != "" {
			return fmt.Errorf("for volume mount type 'ephemeral', source cannot be specified")
		}
		if vm.Propagation != "" {
			return fmt.Errorf("for volume mount type 'ephemeral', propagation cannot be specified")
		}
		if vm.MCSAccessMode != "" {
			return fmt.Errorf("for volume mount type 'ephemeral', mcsaccessshared cannot be specified")
		}
	case "volume":
		if vm.Source != "" {
			return fmt.Errorf("for volume mount type 'volume', source cannot be specified")
		}
		if vm.VolumeName == "" {
			return fmt.Errorf("for volume mount type 'volume', name is required")
		}
		if vm.Propagation != "" {
			return fmt.Errorf("for volume mount type 'ephemeral', propagation cannot be specified")
		}
		if vm.MCSAccessMode != "" {
			return fmt.Errorf("for volume mount type 'ephemeral', mcsaccessshared cannot be specified")
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
	apiVM.VolumeName = vm.VolumeName
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
	case "ephemeral":
		apiVM.Type = api.MountTypeEphemeral
	case "volume":
		apiVM.Type = api.MountTypeVolume
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
	vm.VolumeName = apivm.VolumeName
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
	case api.MountTypeEphemeral:
		vm.Type = "ephemeral"
	case api.MountTypeVolume:
		vm.Type = "volume"
	}
}
