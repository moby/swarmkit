package spec

import (
	"testing"

	"github.com/docker/swarm-v2/api"
	"github.com/stretchr/testify/assert"
)

func TestVolumesValidate(t *testing.T) {
	bad := []*Mount{
		// Only "", RO, RW masks are supported at this time
		{Mask: "unknown"},

		// Only BindHostDir is supported at this time
		{Mask: "ro", Type: "unknown"},

		// With BindHostDir, bith Source and Target have to be specified
		{Mask: "ro", Type: "BindHostDir", Target: "/foo"},
		{Mask: "rw", Type: "BindHostDir", Source: "/foo"},
	}
	good := []*Mount{
		nil,
		{Mask: "rw", Type: "BindHostDir", Target: "/foo", Source: "/foo"},
		{Mask: "rW", Type: "BindHostDir", Target: "/foo", Source: "/foo"},
		{Mask: "Ro", Type: "BindHostDir", Target: "/foo", Source: "/foo"},
		{Mask: "", Type: "BindHostDir", Target: "/foo", Source: "/foo"},
	}

	for _, b := range bad {
		assert.Error(t, b.Validate())
	}

	for _, g := range good {
		assert.NoError(t, g.Validate())
	}
}

func TestVolumesToProto(t *testing.T) {
	type conv struct {
		from *Mount
		to   *api.Mount
	}

	set := []*conv{
		{
			from: nil,
			to:   nil,
		},
		{
			from: &Mount{Mask: "rw", Type: "BindHostDir", Target: "/foo", Source: "/foo"},
			to:   &api.Mount{Mask: "rw", Type: "BindHostDir", Target: "/foo", Source: "/foo"},
		},
	}

	for _, i := range set {
		assert.Equal(t, i.from.ToProto(), i.to)
	}
}

func TestVolumesFromProto(t *testing.T) {
	type conv struct {
		from *api.Mount
		to   *Mount
	}

	set := []*conv{
		{
			from: &api.Mount{Mask: "rw", Type: "BindHostDir", Target: "/foo", Source: "/foo"},
			to:   &Mount{Mask: "rw", Type: "BindHostDir", Target: "/foo", Source: "/foo"},
		},
	}

	for _, i := range set {
		tmp := &Mount{}
		tmp.FromProto(i.from)
		assert.Equal(t, tmp, i.to)
	}
}
