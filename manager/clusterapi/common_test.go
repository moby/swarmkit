package clusterapi

import (
	"testing"

	"github.com/docker/swarm-v2/api"
	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
)

func TestValidateMeta(t *testing.T) {
	type BadMeta struct {
		m *api.Meta
		c codes.Code
	}

	for _, bad := range []BadMeta{
		{
			m: nil,
			c: codes.InvalidArgument,
		},
		{
			m: &api.Meta{},
			c: codes.InvalidArgument,
		},
	} {
		err := validateMeta(bad.m)
		assert.Error(t, err)
		assert.Equal(t, bad.c, grpc.Code(err))
	}

	for _, good := range []*api.Meta{
		{Name: "name"},
	} {
		err := validateMeta(good)
		assert.NoError(t, err)
	}
}
