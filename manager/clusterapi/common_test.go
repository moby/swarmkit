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
		m api.Meta
		c codes.Code
	}

	err := validateMeta(api.Meta{})
	assert.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, grpc.Code(err))

	for _, good := range []api.Meta{
		{Name: "name"},
	} {
		err := validateMeta(good)
		assert.NoError(t, err)
	}
}
