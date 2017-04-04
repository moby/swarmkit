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

const (
	// ThirdPartyResourceFlag defines the third party resource flag
	ThirdPartyResourceFlag = "third-party-resource"
)

func parseResourceCPU(flags *pflag.FlagSet, resources *api.Resources, name string) error {
	cpu, err := flags.GetString(name)
	if err != nil {
		return err
	}

	nanoCPUs, ok := new(big.Rat).SetString(cpu)
	if !ok {
		return fmt.Errorf("invalid cpu: %s", cpu)
	}
	cpuRat := new(big.Rat).Mul(nanoCPUs, big.NewRat(1e9, 1))
	if !cpuRat.IsInt() {
		return fmt.Errorf("CPU value cannot have more than 9 decimal places: %s", cpu)
	}
	resources.NanoCPUs = cpuRat.Num().Int64()
	return nil
}

func newTPRError(format string, args ...interface{}) error {
	return fmt.Errorf("Could not parse TPR: "+format, args...)
}

// ParseThirdPartyResources parses the TPR resources given by the arguments
func ParseThirdPartyResources(flags *pflag.FlagSet, resources *api.Resources, name string) error {
	tprs, err := flags.GetString(name)
	if err != nil {
		return err
	}

	resources.ThirdParty = make(map[string]*api.ThirdPartyResource)
	tpr := resources.ThirdParty
	voidVal := &api.Set_Void{}

	for _, term := range strings.Split(tprs, ";") {
		kva := strings.Split(term, "=")

		if len(kva) != 2 {
			return newTPRError("incorrect term %s, missing '=' or malformed expr", term)
		}

		key := strings.Trim(kva[0], " ")
		val := strings.Trim(kva[1], " ")

		tpr[key] = &api.ThirdPartyResource{}
		u, err := strconv.ParseUint(val, 10, 64)
		if err == nil {
			tpr[key].Resource = &api.ThirdPartyResource_Integer{Integer: &api.Integer{Val: u}}
			continue
		}

		tpr[key].Resource = &api.ThirdPartyResource_Set{Set: &api.Set{Val: make(map[string]*api.Set_Void)}}
		set := tpr[key].Resource.(*api.ThirdPartyResource_Set).Set

		for _, val := range strings.Split(val, ",") {
			set.Val[val] = voidVal
		}
	}

	return nil
}

func validateUserTPR(resources *api.Resources) error {
	for k, v := range resources.ThirdParty {
		switch v.Resource.(type) {
		case *api.ThirdPartyResource_Integer:
			continue
		case *api.ThirdPartyResource_Set:
			return newTPRError("Invalid Argument for resource %s", k)
		}
	}

	return nil
}

func parseResourceMemory(flags *pflag.FlagSet, resources *api.Resources, name string) error {
	memory, err := flags.GetString(name)
	if err != nil {
		return err
	}

	bytes, err := units.RAMInBytes(memory)
	if err != nil {
		return err
	}

	resources.MemoryBytes = bytes
	return nil
}

// NewResources creates a Resources object
func NewResources() *api.Resources {
	return &api.Resources{
		ThirdParty: make(map[string]*api.ThirdPartyResource),
	}
}

func parseResource(flags *pflag.FlagSet, spec *api.ServiceSpec) error {
	if flags.Changed("memory-reservation") {
		if spec.Task.Resources == nil {
			spec.Task.Resources = &api.ResourceRequirements{}
		}
		if spec.Task.Resources.Reservations == nil {
			spec.Task.Resources.Reservations = NewResources()
		}
		if err := parseResourceMemory(flags, spec.Task.Resources.Reservations, "memory-reservation"); err != nil {
			return err
		}
	}

	if flags.Changed("memory-limit") {
		if spec.Task.Resources == nil {
			spec.Task.Resources = &api.ResourceRequirements{}
		}
		if spec.Task.Resources.Limits == nil {
			spec.Task.Resources.Limits = NewResources()
		}
		if err := parseResourceMemory(flags, spec.Task.Resources.Limits, "memory-limit"); err != nil {
			return err
		}
	}

	if flags.Changed("cpu-reservation") {
		if spec.Task.Resources == nil {
			spec.Task.Resources = &api.ResourceRequirements{}
		}
		if spec.Task.Resources.Reservations == nil {
			spec.Task.Resources.Reservations = NewResources()
		}
		if err := parseResourceCPU(flags, spec.Task.Resources.Reservations, "cpu-reservation"); err != nil {
			return err
		}
	}

	if flags.Changed("cpu-limit") {
		if spec.Task.Resources == nil {
			spec.Task.Resources = &api.ResourceRequirements{}
		}
		if spec.Task.Resources.Limits == nil {
			spec.Task.Resources.Limits = NewResources()
		}
		if err := parseResourceCPU(flags, spec.Task.Resources.Limits, "cpu-limit"); err != nil {
			return err
		}
	}

	if flags.Changed(ThirdPartyResourceFlag) {
		if spec.Task.Resources == nil {
			spec.Task.Resources = &api.ResourceRequirements{}
		}
		if spec.Task.Resources.Reservations == nil {
			spec.Task.Resources.Reservations = NewResources()
		}

		err := ParseThirdPartyResources(flags, spec.Task.Resources.Reservations, ThirdPartyResourceFlag)
		if err != nil {
			return err
		}
		err = validateUserTPR(spec.Task.Resources.Reservations)
		if err != nil {
			return err
		}
	}

	return nil
}
