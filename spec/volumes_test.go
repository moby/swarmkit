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
		{Mask: "ro", Type: "bind", Target: "/foo"},
		{Mask: "rw", Type: "bind", Source: "/foo"},

		// Ephemeral => no mask
		{Mask: "ro", Type: "ephemeral"},

		// Ephemeral => no source
		{Type: "ephemeral", Source: "/foo"},

		// Ephemeral => target required
		{Type: "ephemeral"},

		// Volume
		{Mask: "ro", Type: "volume", Target: "/foo"},
		{Mask: "rw", Type: "volume", Source: "/foo"},
	}
	good := []*Mount{
		nil,
		{Mask: "rw", Type: "bind", Target: "/foo", Source: "/foo"},
		{Mask: "rW", Type: "bind", Target: "/foo", Source: "/foo"},
		{Mask: "Ro", Type: "bind", Target: "/foo", Source: "/foo"},
		{Mask: "", Type: "bind", Target: "/foo", Source: "/foo"},

		// Ephemeral
		{Type: "ephemeral", Target: "/foo"},

		{Mask: "Ro", Type: "volume", Target: "/foo", VolumeName: "foo"},
		{Mask: "", Type: "volume", Target: "/foo", VolumeName: "foo"},
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
			from: &Mount{Mask: "rw", Type: "bind", Target: "/foo", Source: "/foo"},
			to:   &api.Mount{Mask: api.MountMaskReadWrite, Type: api.MountTypeBind, Target: "/foo", Source: "/foo"},
		},
		{
			from: &Mount{Type: "ephemeral", Target: "/foo"},
			to:   &api.Mount{Type: api.MountTypeEphemeral, Target: "/foo"},
		},
		{
			from: &Mount{Mask: "rw", Type: "volume", Target: "/foo", VolumeName: "foo"},
			to:   &api.Mount{Mask: api.MountMaskReadWrite, Type: api.MountTypeVolume, Target: "/foo", VolumeName: "foo"},
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
			from: &api.Mount{Mask: api.MountMaskReadWrite, Type: api.MountTypeBind, Target: "/foo", Source: "/foo"},
			to:   &Mount{Mask: "rw", Type: "bind", Target: "/foo", Source: "/foo"},
		},
		{
			from: &api.Mount{Type: api.MountTypeEphemeral, Target: "/foo"},
			to:   &Mount{Type: "ephemeral", Target: "/foo", Mask: "ro"},
		},
		{
			from: &api.Mount{Mask: api.MountMaskReadWrite, Type: api.MountTypeVolume, Target: "/foo", VolumeName: "foo"},
			to:   &Mount{Mask: "rw", Type: "volume", Target: "/foo", VolumeName: "foo"},
		},
	}

	for _, i := range set {
		tmp := &Mount{}
		tmp.FromProto(i.from)
		assert.Equal(t, tmp, i.to)
	}
}
