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

	// Supported types are: BindHostDir, EphemeralDir, ??
	Type string `yaml:"type,omitempty"`

	// Supported masks are: RO, RW (case insensitive)
	Mask string `yaml:"mask,omitempty"`
}

// Mounts - defined to add To/FromProto methods
type Mounts []Mount

// Validate checks the validity of a Mount
func (vm *Mount) Validate() error {
	// Assume everything is alright if mounts are not specified.
	if vm == nil {
		return nil
	}

	if strings.Compare(vm.Type, "BindHostDir") != 0 && strings.Compare(vm.Type, "EphemeralDir") != 0 {
		return fmt.Errorf("valid volume mount types are: BindHostDir, EphemeralDir")
	}

	if vm.Target == "" {
		return fmt.Errorf("target is required")
	}

	if strings.Compare(vm.Type, "BindHostDir") == 0 {
		if vm.Source == "" {
			return fmt.Errorf("For volume mount type 'BindHostDir', source is required")
		}

		// if Mask is not specified, set it to RO
		if vm.Mask == "" {
			vm.Mask = "ro"
		} else {
			vm.Mask = strings.ToLower(vm.Mask)
			if strings.Compare(vm.Mask, "ro") != 0 && strings.Compare(vm.Mask, "rw") != 0 {
				return fmt.Errorf("valid volume mount masks are: ro, rw")
			}
		}

	}
	if strings.Compare(vm.Type, "EphemeralDir") == 0 {
		if vm.Source != "" {
			return fmt.Errorf("For volume mount type 'EphemeralDir', source cannot be specified")
		}
		if vm.Mask != "" {
			return fmt.Errorf("For volume mount type 'EphemeralDir', mask cannot be specified")
		}
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
	apiVM.Mask = vm.Mask
	if strings.Compare(vm.Type, "BindHostDir") == 0 {
		apiVM.Type = api.Mount_BIND
	} else if strings.Compare(vm.Type, "EphemeralDir") == 0 {
		apiVM.Type = api.Mount_EPHEMERAL
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
	vm.Mask = apivm.Mask
	if apivm.Type == api.Mount_BIND {
		vm.Type = "BindHostDir"
	} else if apivm.Type == api.Mount_EPHEMERAL {
		vm.Type = "EphemeralDir"
	}
}
