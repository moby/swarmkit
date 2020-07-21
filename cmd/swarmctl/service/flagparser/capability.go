package flagparser

import (
	"github.com/docker/swarmkit/api"
	"github.com/spf13/cobra"
)

// ParseAddCapability validates capabilities passed on the command line
func ParseAddCapability(cmd *cobra.Command, spec *api.ServiceSpec, flagName string) error {
	flags := cmd.Flags()

	if !flags.Changed(flagName) {
		return nil
	}

	add, err := flags.GetStringSlice(flagName)
	if err != nil {
		return err
	}

	container := spec.Task.GetContainer()
	if container == nil {
		return nil
	}

	// Index adds so we don't have to double loop
	addIndex := make(map[string]bool, len(add))
	for _, v := range add {
		addIndex[v] = true
	}

	// Check if any of the adds are in drop so we can remove them from the drop list.
	var evict []int
	for i, v := range container.CapabilityDrop {
		if addIndex[v] {
			evict = append(evict, i)
		}
	}
	for n, i := range evict {
		container.CapabilityDrop = append(container.CapabilityDrop[:i-n], container.CapabilityDrop[i-n+1:]...)
	}

	// De-dup the list to be added
	for _, v := range container.CapabilityAdd {
		if addIndex[v] {
			delete(addIndex, v)
			continue
		}
	}

	for cap := range addIndex {
		container.CapabilityAdd = append(container.CapabilityAdd, cap)
	}

	return nil
}

// ParseDropCapability validates capabilities passed on the command line
func ParseDropCapability(cmd *cobra.Command, spec *api.ServiceSpec, flagName string) error {
	flags := cmd.Flags()

	if !flags.Changed(flagName) {
		return nil
	}

	drop, err := flags.GetStringSlice(flagName)
	if err != nil {
		return err
	}

	container := spec.Task.GetContainer()
	if container == nil {
		return nil
	}

	// Index removals so we don't have to double loop
	dropIndex := make(map[string]bool, len(drop))
	for _, v := range drop {
		dropIndex[v] = true
	}

	// Check if any of the adds are in add so we can remove them from the add list.
	var evict []int
	for i, v := range container.CapabilityAdd {
		if dropIndex[v] {
			evict = append(evict, i)
		}
	}
	for n, i := range evict {
		container.CapabilityAdd = append(container.CapabilityAdd[:i-n], container.CapabilityAdd[i-n+1:]...)
	}

	// De-dup the list to be dropped
	for _, v := range container.CapabilityDrop {
		if dropIndex[v] {
			delete(dropIndex, v)
			continue
		}
	}

	for cap := range dropIndex {
		container.CapabilityDrop = append(container.CapabilityDrop, cap)
	}

	return nil
}
