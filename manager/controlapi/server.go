package controlapi

import (
	"errors"
	"sync"

	"github.com/docker/swarmkit/manager/state/raft"
	"github.com/docker/swarmkit/manager/state/store"
)

var (
	errNotImplemented  = errors.New("not implemented")
	errInvalidArgument = errors.New("invalid argument")
)

// Server is the Cluster API gRPC server.
type Server struct {
	store      *store.MemoryStore
	raft       *raft.Node
	updateLock sync.Mutex
}

// NewServer creates a Cluster API server.
func NewServer(store *store.MemoryStore, raft *raft.Node) *Server {
	return &Server{
		store: store,
		raft:  raft,
	}
}
