package spec

import (
	"fmt"
	"strings"

	"github.com/docker/swarm-v2/api"
)

// Mount is a human representation of VolumeSpec & contains volumes to be mounted into the Task
type Mount struct {
	// Location for mount inside the container
	Target string `toml:"target,omitempty"`

	// Source directory to be mounted
	Source string `toml:"source,omitempty"`

	// Name of plugin volume
	VolumeName string `toml:"name,omitempty"`

	// Supported types are: bind, ephemeral, volume
	Type string `toml:"type,omitempty"`

	// Supported masks are: RO, RW (case insensitive)
	Mask string `toml:"mask,omitempty"`
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

		switch strings.ToLower(vm.Mask) {
		case "", "ro", "rw":
		default:
			return fmt.Errorf("valid volume mount masks are: ro, rw")
		}
	case "ephemeral":
		if vm.Source != "" {
			return fmt.Errorf("for volume mount type 'ephemeral', source cannot be specified")
		}
		if vm.Mask != "" {
			return fmt.Errorf("for volume mount type 'ephemeral', mask cannot be specified")
		}
	case "volume":
		if vm.Source != "" {
			return fmt.Errorf("for volume mount type 'volume', source cannot be specified")
		}
		if vm.VolumeName == "" {
			return fmt.Errorf("for volume mount type 'volume', name is required")
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
	switch strings.ToLower(vm.Mask) {
	case "ro":
		apiVM.Mask = api.MountMaskReadOnly
	case "rw":
		apiVM.Mask = api.MountMaskReadWrite
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
	switch apivm.Mask {
	case api.MountMaskReadOnly:
		vm.Mask = "ro"
	case api.MountMaskReadWrite:
		vm.Mask = "rw"
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
