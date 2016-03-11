package clusterapi

import (
	"errors"

	"github.com/docker/swarm-v2/manager/state"
)

var (
	errNotImplemented  = errors.New("not implemented")
	errInvalidArgument = errors.New("invalid argument")
)

// Server is the Cluster API gRPC server.
type Server struct {
	store state.Store
}

// NewServer creates a Cluster API server.
func NewServer(store state.WatchableStore) *Server {
	return &Server{
		store: store,
	}
}
