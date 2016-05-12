package controlapi

import (
	"testing"

	"github.com/docker/swarm-v2/api"
	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
)

func TestValidateAnnotations(t *testing.T) {
	type BadAnnotations struct {
		m api.Annotations
		c codes.Code
	}

	err := validateAnnotations(api.Annotations{})
	assert.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, grpc.Code(err))

	for _, good := range []api.Annotations{
		{Name: "name"},
	} {
		err := validateAnnotations(good)
		assert.NoError(t, err)
	}
}
