package clusterapi

import (
	"errors"

	"github.com/docker/swarm-v2/manager/state"
	"github.com/docker/swarm-v2/manager/state/raft"
)

var (
	errNotImplemented  = errors.New("not implemented")
	errInvalidArgument = errors.New("invalid argument")
)

// Server is the Cluster API gRPC server.
type Server struct {
	store state.Store
	raft  *raft.Node
}

// NewServer creates a Cluster API server.
func NewServer(store state.WatchableStore, raft *raft.Node) *Server {
	return &Server{
		store: store,
		raft:  raft,
	}
}
