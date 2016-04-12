package clusterapi

import (
	"testing"

	specspb "github.com/docker/swarm-v2/pb/docker/cluster/specs"
	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
)

func TestValidateMeta(t *testing.T) {
	type BadMeta struct {
		m specspb.Meta
		c codes.Code
	}

	err := validateMeta(specspb.Meta{})
	assert.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, grpc.Code(err))

	for _, good := range []specspb.Meta{
		{Name: "name"},
	} {
		err := validateMeta(good)
		assert.NoError(t, err)
	}
}
