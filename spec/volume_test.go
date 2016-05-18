package spec

import (
	"testing"

	"github.com/docker/swarm-v2/api"
	"github.com/stretchr/testify/assert"
)

func TestVolumesValidate(t *testing.T) {
	bad := []*VolumeConfig{
		{Name: ""},
		{Name: "foo", Driver: ""},
		{Name: "foo", Driver: "bar", DriverOpts: []string{"a"}},
		{Name: "foo", Driver: "bar", DriverOpts: []string{"a==b"}},
	}
	good := []*VolumeConfig{
		nil,
		{Name: "foo", Driver: "bar"},
		{Name: "foo", Driver: "bar", DriverOpts: []string{"a=z"}},
		{Name: "foo", Driver: "bar", DriverOpts: []string{"a=z", "b=z", "c=z"}},
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
		from *VolumeConfig
		to   *api.VolumeSpec
	}

	set := []*conv{
		{
			from: nil,
			to:   nil,
		},
		{
			from: &VolumeConfig{Name: "foo", Driver: "bar"},
			to: &api.VolumeSpec{
				Annotations:         api.Annotations{Name: "foo", Labels: make(map[string]string)},
				DriverConfiguration: &api.Driver{Name: "bar", Options: map[string]string{}},
			},
		},
		{
			from: &VolumeConfig{Name: "foo", Driver: "bar", DriverOpts: []string{"o1=v1"}},
			to: &api.VolumeSpec{
				Annotations:         api.Annotations{Name: "foo", Labels: make(map[string]string)},
				DriverConfiguration: &api.Driver{Name: "bar", Options: map[string]string{"o1": "v1"}},
			},
		},
	}

	for _, i := range set {
		assert.Equal(t, i.from.ToProto(), i.to)
	}
}

func TestVolumesFromProto(t *testing.T) {
	type conv struct {
		from *api.VolumeSpec
		to   *VolumeConfig
	}

	set := []*conv{
		{
			from: &api.VolumeSpec{
				Annotations:         api.Annotations{Name: "foo", Labels: make(map[string]string)},
				DriverConfiguration: &api.Driver{Name: "bar"},
			},
			to: &VolumeConfig{Name: "foo", Driver: "bar"},
		},
		{
			from: &api.VolumeSpec{
				Annotations:         api.Annotations{Name: "foo", Labels: make(map[string]string)},
				DriverConfiguration: &api.Driver{Name: "bar", Options: map[string]string{"o1": "v1"}},
			},
			to: &VolumeConfig{Name: "foo", Driver: "bar", DriverOpts: []string{"o1=v1"}},
		},
	}

	for _, i := range set {
		tmp := &VolumeConfig{}
		tmp.FromProto(i.from)
		assert.Equal(t, tmp, i.to)
	}
}
