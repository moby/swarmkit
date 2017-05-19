package cnm

import (
	"testing"

	"github.com/docker/swarmkit/api"
	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
)

func TestValidateDriver(t *testing.T) {
	nm := New(nil)
	assert.NoError(t, nm.ValidateDriver(nil, ""))

	err := nm.ValidateDriver(&api.Driver{Name: ""}, "")
	assert.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, grpc.Code(err))
}
