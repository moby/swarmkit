package spec

import (
	"fmt"
	"testing"

	"github.com/docker/libswarm/api"
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
		{Type: "bind", Source: "/foo", Target: "/foo", Template: &VolumeTemplate{Name: "foo", Driver: Driver{Name: "bar"}}},

		// Volume
		{Type: "volume", Source: "/foo"}, // volume shouldn't specify source.
		{Type: "volume", Source: "/foo", Target: "/foo", Propagation: "notempty"},

		// Propagation and MCSAccessMode are not allowed for volumes.
		{Type: "volume", Target: "/foo", MCSAccessMode: "shared", Template: &VolumeTemplate{Name: "foo"}},
		{Type: "volume", Target: "/foo", Propagation: "shared"},
		{Type: "volume", Target: "/foo", Propagation: "shared", Template: &VolumeTemplate{Name: "foo", Driver: Driver{Name: "bar"}}},
	}
	good := []*Mount{
		nil,
		{Type: "volume", Target: "/foo"},
		{Type: "volume", Target: "/foo", Populate: true, Template: &VolumeTemplate{Name: "foo"}},

		{Type: "bind", Writable: true, Target: "/foo", Source: "/foo"},
		{Type: "bind", Writable: false, Target: "/foo", Source: "/foo"},
		{Type: "bind", Target: "/foo", Source: "/foo"},
		{Type: "bind", Source: "/foo", Target: "/foo", MCSAccessMode: "shared"},
	}

	for i, b := range bad {
		assert.NotNil(t, b.Validate(), fmt.Sprintf("testcase %v should have failed: %v", i, b))
	}

	for i, g := range good {
		assert.NoError(t, g.Validate(), fmt.Sprintf("testcase %v failed: %v", i, g))
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
			from: &Mount{Type: "volume", Target: "/foo", Populate: true},
			to:   &api.Mount{Type: api.MountTypeVolume, Target: "/foo", Populate: true},
		},
		{
			from: &Mount{Writable: true, Type: "volume", Target: "/foo", Template: &VolumeTemplate{Name: "foo"}, Populate: true},
			to:   &api.Mount{Writable: true, Type: api.MountTypeVolume, Target: "/foo", Template: &api.VolumeTemplate{Annotations: api.Annotations{Name: "foo"}}, Populate: true},
		},
		{
			from: &Mount{Writable: true, Type: "volume", Target: "/foo", Populate: true, Template: &VolumeTemplate{Name: "foo", Driver: Driver{Name: "bar"}}},
			to:   &api.Mount{Writable: true, Type: api.MountTypeVolume, Target: "/foo", Populate: true, Template: &api.VolumeTemplate{Annotations: api.Annotations{Name: "foo"}, DriverConfig: &api.Driver{Name: "bar"}}},
		},
	}

	for i, testcase := range set {
		assert.Equal(t, testcase.to, testcase.from.ToProto(), fmt.Sprintf("testcase %v failed", i))
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
			from: &api.Mount{Type: api.MountTypeVolume, Target: "/foo"},
			to:   &Mount{Type: "volume", Target: "/foo", Writable: false, Propagation: "rprivate", Populate: false},
		},
		{
			from: &api.Mount{Writable: true, Type: api.MountTypeVolume, Target: "/foo", Populate: true, Template: &api.VolumeTemplate{Annotations: api.Annotations{Name: "foo"}, DriverConfig: &api.Driver{Name: "foo", Options: map[string]string{}}}},
			to:   &Mount{Writable: true, Type: "volume", Target: "/foo", Propagation: "rprivate", Populate: true, Template: &VolumeTemplate{Name: "foo", Driver: Driver{Name: "foo"}}},
		},
		{
			from: &api.Mount{Writable: true, Type: api.MountTypeVolume, Target: "/foo", Populate: true, Template: &api.VolumeTemplate{Annotations: api.Annotations{Name: "foo"}, DriverConfig: &api.Driver{Name: "bar", Options: map[string]string{}}}},
			to:   &Mount{Writable: true, Type: "volume", Target: "/foo", Propagation: "rprivate", Populate: true, Template: &VolumeTemplate{Name: "foo", Driver: Driver{Name: "bar"}}},
		},
	}

	for i, testcase := range set {
		tmp := &Mount{}
		tmp.FromProto(testcase.from)
		assert.Equal(t, testcase.to, tmp, fmt.Sprintf("testcase %v failed", i))
	}
}
