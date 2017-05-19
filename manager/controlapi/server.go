package controlapi

import (
	"errors"

	"github.com/docker/swarmkit/ca"
	"github.com/docker/swarmkit/manager/network"
	"github.com/docker/swarmkit/manager/state/raft"
	"github.com/docker/swarmkit/manager/state/store"
)

var (
	errInvalidArgument = errors.New("invalid argument")
)

// Server is the Cluster API gRPC server.
type Server struct {
	store          *store.MemoryStore
	raft           *raft.Node
	securityConfig *ca.SecurityConfig
	nm             network.Model
}

// NewServer creates a Cluster API server.
func NewServer(store *store.MemoryStore, raft *raft.Node, securityConfig *ca.SecurityConfig, nm network.Model) *Server {
	return &Server{
		store:          store,
		raft:           raft,
		securityConfig: securityConfig,
		nm:             nm,
	}
}
