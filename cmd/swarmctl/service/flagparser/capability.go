package flagparser

import (
	"github.com/docker/swarmkit/api"
	"github.com/spf13/cobra"
)

// ParseAddCapability validates capabilities passed on the command line
func ParseAddCapability(cmd *cobra.Command, spec *api.ServiceSpec, flagName string) error {
	flags := cmd.Flags()

	if flags.Changed(flagName) {
		capabilities, err := flags.GetStringSlice(flagName)
		if err != nil {
			return err
		}

		container := spec.Task.GetContainer()
		if container == nil {
			return nil
		}

		oldCapabilities := make(map[string]struct{})
		for _, capability := range container.Capabilities {
			oldCapabilities[capability] = struct{}{}
		}

		var newCapabilities = container.Capabilities
		for _, capability := range capabilities {
			if _, ok := oldCapabilities[capability]; ok {
				continue
			}
			newCapabilities = append(newCapabilities, capability)
		}
		container.Capabilities = newCapabilities
	}

	return nil
}

// ParseRemoveCapability removes a set of capabilities from the task spec's capability references
func ParseRemoveCapability(cmd *cobra.Command, spec *api.ServiceSpec, flagName string) error {
	flags := cmd.Flags()

	if flags.Changed(flagName) {
		capabilities, err := flags.GetStringSlice(flagName)
		if err != nil {
			return err
		}

		container := spec.Task.GetContainer()
		if container == nil {
			return nil
		}

		wantToDelete := make(map[string]struct{})
		for _, capability := range capabilities {
			wantToDelete[capability] = struct{}{}
		}

		var newCapabilities []string
		for _, capabilityRef := range container.Capabilities {
			if _, ok := wantToDelete[capabilityRef]; ok {
				continue
			}
			newCapabilities = append(newCapabilities, capabilityRef)
		}
		container.Capabilities = newCapabilities
	}

	return nil
}
