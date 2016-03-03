package clusterapi

import (
	"testing"

	"github.com/docker/swarm-v2/api"
	"github.com/stretchr/testify/assert"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
)

func TestUpdateNode(t *testing.T) {
	ts := newTestServer(t)
	_, err := ts.Client.UpdateNode(context.Background(), &api.UpdateNodeRequest{})
	assert.Error(t, err)
	assert.Equal(t, codes.Unimplemented, grpc.Code(err))
}

func TestListNodes(t *testing.T) {
	// TODO
}
