package clusterapi

import (
	"testing"

	"github.com/docker/swarm-v2/api"
	"github.com/stretchr/testify/assert"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
)

func TestCreateTask(t *testing.T) {
	ts := newTestServer(t)
	_, err := ts.Client.CreateTask(context.Background(), &api.CreateTaskRequest{})
	assert.Error(t, err)
	assert.Equal(t, codes.Unimplemented, grpc.Code(err))
}

func TestGetTask(t *testing.T) {
	// TODO
}

func TestDeleteTask(t *testing.T) {
	// TODO
}

func TestListTasks(t *testing.T) {
	// TODO
}
