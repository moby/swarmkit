package spec

import (
	"testing"

	"github.com/docker/libswarm/api"
	"github.com/stretchr/testify/assert"
)

func TestResourcesValidate(t *testing.T) {
	bad := []*Resources{
		{Memory: "foo"},
		{Memory: "12 FB"},
	}
	good := []*Resources{
		{CPU: "0", Memory: ""},
		{CPU: "0.1"},
		{CPU: "4"},
		{Memory: "1MiB"},
	}

	for _, b := range bad {
		assert.Error(t, b.Validate())
	}

	for _, g := range good {
		assert.NoError(t, g.Validate())
	}
}

func TestResourceRequirementsValidate(t *testing.T) {
	bad := []*ResourceRequirements{
		{Limits: &Resources{Memory: "invalid"}},
		{Reservations: &Resources{Memory: "invalid"}},
		{Limits: &Resources{Memory: "invalid"}, Reservations: &Resources{Memory: "invalid"}},
	}
	for _, b := range bad {
		assert.Error(t, b.Validate())
	}
}

func TestResourcesToProto(t *testing.T) {
	type conv struct {
		from *Resources
		to   *api.Resources
	}

	set := []*conv{
		{from: nil, to: nil},
		{from: &Resources{CPU: "1", Memory: "1024"}, to: &api.Resources{NanoCPUs: 1e9, MemoryBytes: 1024}},
		{from: &Resources{CPU: "1", Memory: "1KiB"}, to: &api.Resources{NanoCPUs: 1e9, MemoryBytes: 1024}},
	}

	for _, i := range set {
		assert.Equal(t, i.from.ToProto(), i.to)
	}
}

func TestResourcesFromProto(t *testing.T) {
	type conv struct {
		from *api.Resources
		to   *Resources
	}

	set := []*conv{
		{from: &api.Resources{NanoCPUs: 1e9, MemoryBytes: 1024}, to: &Resources{CPU: "1", Memory: "1.0 KiB"}},
	}

	for _, i := range set {
		tmp := &Resources{}
		tmp.FromProto(i.from)
		assert.Equal(t, tmp, i.to)
	}
}

func TestResourceRequirementsMaintainUnset(t *testing.T) {
	type conv struct {
		from *ResourceRequirements
		to   *api.ResourceRequirements
	}

	set := []*conv{
		{from: nil, to: nil},
		{from: &ResourceRequirements{Limits: nil, Reservations: nil}, to: &api.ResourceRequirements{Limits: nil, Reservations: nil}},
		{from: &ResourceRequirements{Limits: &Resources{CPU: "1", Memory: "1 B"}, Reservations: nil}, to: &api.ResourceRequirements{Limits: &api.Resources{NanoCPUs: 1e9, MemoryBytes: 1}, Reservations: nil}},
		{from: &ResourceRequirements{Limits: nil, Reservations: &Resources{CPU: "1", Memory: "1 B"}}, to: &api.ResourceRequirements{Limits: nil, Reservations: &api.Resources{NanoCPUs: 1e9, MemoryBytes: 1}}},
		{from: &ResourceRequirements{Limits: &Resources{CPU: "1", Memory: "1 B"}, Reservations: &Resources{CPU: "1", Memory: "1 B"}}, to: &api.ResourceRequirements{Limits: &api.Resources{NanoCPUs: 1e9, MemoryBytes: 1}, Reservations: &api.Resources{NanoCPUs: 1e9, MemoryBytes: 1}}},
	}

	for _, i := range set {
		assert.Equal(t, i.from.ToProto(), i.to)
	}

	for _, i := range set {
		if i.from == nil {
			continue
		}
		tmp := &ResourceRequirements{}
		tmp.FromProto(i.to)
		assert.Equal(t, tmp, i.from)
	}
}
