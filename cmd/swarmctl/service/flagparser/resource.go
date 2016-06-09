package flagparser

import (
	"fmt"
	"math/big"
	"strconv"
	"strings"

	"github.com/docker/go-units"
	"github.com/docker/swarmkit/api"
	"github.com/spf13/pflag"
)

var (
	// string name of pre-defined resources
	cpu         = api.CPUs.String()
	memory      = api.Memory.String()
	nanoCPUs    = api.NanoCPUs.String()
	memoryBytes = api.MemoryBytes.String()
)

// ParseUserDefinedResources parses user-defined resources from string to api.Resources
func ParseUserDefinedResources(flags *pflag.FlagSet, resources *api.Resources, name string, isLimit bool) error {
	resourcesStr, err := flags.GetStringSlice(name)
	if err != nil {
		return err
	}

	if len(resourcesStr) == 0 {
		return nil
	}

	// convert all ResourcesName to lower case and check
	// 0. if resource type is scalar
	// 1. if re-define resources
	// 2. validate pre-defined resources
	// 3. validate if other user-defined resources value is number
	definedResources := make(map[string]bool, len(resourcesStr))
	for _, resource := range resourcesStr {
		parts := strings.Split(resource, "=")
		if len(parts) != 2 {
			return fmt.Errorf("error format in resources configuration, please define as [name]=[value]")
		}

		newName := strings.ToLower(parts[0])
		value := parts[1]
		if _, ok := definedResources[newName]; ok {
			return fmt.Errorf("re-define resource: %s", parts[0])
		}
		definedResources[newName] = true

		if newName == cpu {
			if _, ok := definedResources[nanoCPUs]; ok {
				return fmt.Errorf("re-define resources: %s and %s", cpu, nanoCPUs)
			}
			definedResources[nanoCPUs] = true

			nanoCount, ok := new(big.Rat).SetString(value)
			if !ok {
				return fmt.Errorf("invalid cpu: %s", value)
			}
			cpuRat := new(big.Rat).Mul(nanoCount, big.NewRat(1e9, 1))
			if !cpuRat.IsInt() {
				return fmt.Errorf("CPU value cannot have more than 9 decimal places: %s", value)
			}
			resources.ScalarResources[nanoCPUs] = float64(cpuRat.Num().Int64())
		} else if newName == memory {
			if _, ok := definedResources[memoryBytes]; ok {
				return fmt.Errorf("re-define resources: %s and %s", memory, memoryBytes)
			}
			definedResources[memoryBytes] = true

			bytes, err := units.RAMInBytes(value)
			if err != nil {
				return err
			}

			resources.ScalarResources[memoryBytes] = float64(bytes)
		} else {
			if isLimit {
				// only memory and cpu are supported in Limits
				return fmt.Errorf("only memory and cpu resources are supported in limit")
			}
			v, err := strconv.ParseFloat(value, 64)
			if err != nil {
				return fmt.Errorf("can't parse resource %s", parts[0])
			}
			resources.ScalarResources[newName] = v
		}
	}

	return nil
}

func parseResource(flags *pflag.FlagSet, spec *api.ServiceSpec) error {
	if flags.Changed("reservation") {
		if spec.Task.Resources == nil {
			spec.Task.Resources = &api.ResourceRequirements{}
		}
		if spec.Task.Resources.Reservations == nil {
			spec.Task.Resources.Reservations = &api.Resources{}
		}
		if spec.Task.Resources.Reservations.ScalarResources == nil {
			spec.Task.Resources.Reservations.ScalarResources = map[string]float64{}
		}
		if err := ParseUserDefinedResources(flags, spec.Task.Resources.Reservations, "reservation", false); err != nil {
			return err
		}
	}

	if flags.Changed("limit") {
		if spec.Task.Resources == nil {
			spec.Task.Resources = &api.ResourceRequirements{}
		}
		if spec.Task.Resources.Limits == nil {
			spec.Task.Resources.Limits = &api.Resources{}
		}
		if spec.Task.Resources.Limits.ScalarResources == nil {
			spec.Task.Resources.Limits.ScalarResources = map[string]float64{}
		}
		if err := ParseUserDefinedResources(flags, spec.Task.Resources.Limits, "limit", true); err != nil {
			return err
		}
	}

	return nil
}
