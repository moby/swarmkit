package spec

import (
	"testing"

	"github.com/docker/swarm-v2/api"
	"github.com/stretchr/testify/assert"
)

func TestMountsValidate(t *testing.T) {
	bad := []*Mount{
		// Only BindHostDir is supported at this time
		{Type: "unknown"},

		// With BindHostDir, both Source and Target have to be specified
		{Type: "bind", Target: "/foo"},
		{Type: "bind", Source: "/foo"},
		{Type: "bind", Source: "/foo", Target: "/foo", Propagation: "unknown"},
		{Type: "bind", Source: "/foo", Target: "/foo", MCSAccessMode: "unknown"},
		{Type: "bind", Source: "/foo", Target: "/foo", Populate: true},

		// Ephemeral => no source
		{Type: "ephemeral", Source: "/foo"},

		// Ephemeral => target required
		{Type: "ephemeral"},
		// Can't set MCSMode
		{Type: "ephemeral", Target: "/foo", MCSAccessMode: "shared"},
		{Type: "ephemeral", Target: "/foo", Propagation: "shared"},

		// Volume
		{Type: "volume", Target: "/foo"},
		{Type: "volume", Source: "/foo"},
		{Type: "volume", Source: "/foo", Target: "/foo", Propagation: "notempty"},
		{Type: "volume", Target: "/foo", VolumeName: "foo", MCSAccessMode: "shared"},
		{Type: "volume", Target: "/foo", VolumeName: "foo", Propagation: "shared"},
	}
	good := []*Mount{
		nil,
		{Writable: true, Type: "bind", Target: "/foo", Source: "/foo"},
		{Writable: false, Type: "bind", Target: "/foo", Source: "/foo"},
		{Type: "bind", Target: "/foo", Source: "/foo"},
		{Type: "bind", Source: "/foo", Target: "/foo", MCSAccessMode: "shared"},

		// Ephemeral
		{Type: "ephemeral", Target: "/foo", Populate: true},

		{Type: "volume", Target: "/foo", VolumeName: "foo", Populate: true},
	}

	for _, b := range bad {
		assert.Error(t, b.Validate())
	}

	for _, g := range good {
		assert.NoError(t, g.Validate())
	}
}

func TestMountsToProto(t *testing.T) {
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
			from: &Mount{Writable: true, Type: "bind", Target: "/foo", Source: "/foo"},
			to:   &api.Mount{Writable: true, Type: api.MountTypeBind, Target: "/foo", Source: "/foo"},
		},
		{
			from: &Mount{Type: "ephemeral", Target: "/foo", Populate: true},
			to:   &api.Mount{Type: api.MountTypeEphemeral, Target: "/foo", Populate: true},
		},
		{
			from: &Mount{Writable: true, Type: "volume", Target: "/foo", VolumeName: "foo", Populate: true},
			to:   &api.Mount{Writable: true, Type: api.MountTypeVolume, Target: "/foo", VolumeName: "foo", Populate: true},
		},
	}

	for _, i := range set {
		assert.Equal(t, i.from.ToProto(), i.to)
	}
}

func TestMountsFromProto(t *testing.T) {
	type conv struct {
		from *api.Mount
		to   *Mount
	}

	set := []*conv{
		{
			from: &api.Mount{Writable: true, Type: api.MountTypeBind, Target: "/foo", Source: "/foo", Mcsaccessmode: api.MountMCSAccessModeShared},
			to:   &Mount{Writable: true, Type: "bind", Target: "/foo", Source: "/foo", Propagation: "rprivate", MCSAccessMode: "shared"},
		},
		{
			from: &api.Mount{Type: api.MountTypeEphemeral, Target: "/foo"},
			to:   &Mount{Type: "ephemeral", Target: "/foo", Writable: false, Propagation: "rprivate", Populate: false},
		},
		{
			from: &api.Mount{Writable: true, Type: api.MountTypeVolume, Target: "/foo", VolumeName: "foo", Populate: true},
			to:   &Mount{Writable: true, Type: "volume", Target: "/foo", VolumeName: "foo", Propagation: "rprivate", Populate: true},
		},
	}

	for _, i := range set {
		tmp := &Mount{}
		tmp.FromProto(i.from)
		assert.Equal(t, tmp, i.to)
	}
}
