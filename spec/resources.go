package spec

import (
	"fmt"
	"math/big"
	"strings"

	"github.com/docker/swarmkit/api"
	"github.com/dustin/go-humanize"
)

func stripTrailingZeros(s string) string {
	// s might be something like 10.000000000.
	// We have to trim the trailing 0s then the trailing dot.
	return strings.TrimRight(
		strings.TrimRight(s, "0"),
		".",
	)
}

// Resources represent a set of various resources.
type Resources struct {
	CPU    string `yaml:"cpu,omitempty"`
	Memory string `yaml:"memory,omitempty"`
}

// Validate checks the validity of Resources.
func (r *Resources) Validate() error {
	// Assume everything is alright if resoruces are not specified.
	if r.CPU == "" && r.Memory == "" {
		return nil
	}

	if r.CPU != "" {
		cpu, ok := new(big.Rat).SetString(r.CPU)
		if !ok {
			return fmt.Errorf("invalid CPU value: %s", r.CPU)
		}
		nanoCPUs := new(big.Rat).Mul(cpu, big.NewRat(1e9, 1))
		if !nanoCPUs.IsInt() {
			return fmt.Errorf("CPU value cannot have more than 9 decimal places: %s", r.CPU)
		}
	}

	if r.Memory != "" {
		if _, err := humanize.ParseBytes(r.Memory); err != nil {
			return err
		}
	}

	return nil
}

// ToProto converts native Resources into protos.
func (r *Resources) ToProto() *api.Resources {
	if r == nil {
		return nil
	}

	p := &api.Resources{}

	// Skip error checking here - `Validate` must have been called before.
	if r.CPU != "" {
		cpu, _ := new(big.Rat).SetString(r.CPU)
		p.NanoCPUs = cpu.Mul(cpu, big.NewRat(1e9, 1)).Num().Int64()
	}

	if r.Memory != "" {
		bytes, _ := humanize.ParseBytes(r.Memory)
		p.MemoryBytes = int64(bytes)
	}

	return p
}

// FromProto converts proto Resources back into native types.
func (r *Resources) FromProto(p *api.Resources) {
	if p == nil {
		return
	}

	*r = Resources{}
	if p.NanoCPUs != 0 {
		r.CPU = stripTrailingZeros(big.NewRat(p.NanoCPUs, 1e9).FloatString(9))
	}
	if p.MemoryBytes != 0 {
		r.Memory = humanize.IBytes(uint64(p.MemoryBytes))
	}
}

// ResourceRequirements defines requirements for a container.
// Limits: Maximum amount of resources a container can use.
// Reservations: Reserved amount of resources on the node.
type ResourceRequirements struct {
	Limits       *Resources `yaml:"limits,omitempty"`
	Reservations *Resources `yaml:"reservations,omitempty"`
}

// Validate checks the validity of the resource requirements.
func (r *ResourceRequirements) Validate() error {
	if r == nil {
		return nil
	}
	if r.Limits != nil {
		if err := r.Limits.Validate(); err != nil {
			return err
		}
	}
	if r.Reservations != nil {
		if err := r.Reservations.Validate(); err != nil {
			return err
		}
	}
	return nil
}

// ToProto converts native ResourceRequirements into protos.
func (r *ResourceRequirements) ToProto() *api.ResourceRequirements {
	if r == nil {
		return nil
	}
	return &api.ResourceRequirements{
		Limits:       r.Limits.ToProto(),
		Reservations: r.Reservations.ToProto(),
	}
}

// FromProto converts proto ResourceRequirements back into native types.
func (r *ResourceRequirements) FromProto(p *api.ResourceRequirements) {
	if p == nil {
		return
	}

	*r = ResourceRequirements{}
	if p.Limits != nil {
		r.Limits = &Resources{}
		r.Limits.FromProto(p.Limits)
	}
	if p.Reservations != nil {
		r.Reservations = &Resources{}
		r.Reservations.FromProto(p.Reservations)
	}
}
