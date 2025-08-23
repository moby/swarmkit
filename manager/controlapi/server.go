package controlapi

import (
	"errors"

	"github.com/moby/swarmkit/v2/ca"
	"github.com/moby/swarmkit/v2/manager/allocator/networkallocator"
	"github.com/moby/swarmkit/v2/manager/drivers"
	"github.com/moby/swarmkit/v2/manager/state/raft"
	"github.com/moby/swarmkit/v2/manager/state/store"
)

var (
	errInvalidArgument = errors.New("invalid argument")
)

// Server is the Cluster API gRPC server.
type Server struct {
	store          *store.MemoryStore
	raft           *raft.Node
	securityConfig *ca.SecurityConfig
	netvalidator   networkallocator.DriverValidator
	viewhooks      ViewResponseMutator
	dr             *drivers.DriverProvider
}

// NewServer creates a Cluster API server.
func NewServer(store *store.MemoryStore, raft *raft.Node, securityConfig *ca.SecurityConfig, nv networkallocator.DriverValidator, dr *drivers.DriverProvider) *Server {
	opts := []ServerOption{
		WithMemoryStore(store),
		WithRaftNode(raft),
		WithSecurityConfig(securityConfig),
	}
	if nv != nil {
		opts = append(opts, WithNetworkDriverValidator(nv))
	}
	if dr != nil {
		opts = append(opts, WithDriverProvider(dr))
	}
	srv, err := New(opts...)
	if err != nil {
		// TODO(thaJeztah): preserving the old behavior; should this panic instead?
		return &Server{}
	}
	return srv
}

// New creates a Cluster API server.
func New(opts ...ServerOption) (*Server, error) {
	var s Server

	for _, opt := range opts {
		if err := opt(&s); err != nil {
			return nil, err
		}
	}

	if s.store == nil {
		return nil, errors.New("no memory store provided")
	}
	if s.securityConfig == nil {
		return nil, errors.New("no securityConfig provided")
	}
	if s.netvalidator == nil {
		s.netvalidator = networkallocator.InertProvider{}
	}
	if s.viewhooks == nil {
		s.viewhooks = NoopViewResponseMutator{}
	}
	if s.dr == nil {
		s.dr = &drivers.DriverProvider{}
	}
	return &s, nil
}

// ServerOption is a functional argument to configure a new server.
type ServerOption func(*Server) error

// WithMemoryStore configures the server's memory store.
func WithMemoryStore(store *store.MemoryStore) ServerOption {
	return func(s *Server) error {
		s.store = store
		return nil
	}
}

// WithRaftNode configures the server's raft node.
func WithRaftNode(raft *raft.Node) ServerOption {
	return func(s *Server) error {
		s.raft = raft
		return nil
	}
}

// WithDriverProvider configures the server's driver provider.
func WithDriverProvider(dr *drivers.DriverProvider) ServerOption {
	return func(s *Server) error {
		s.dr = dr
		return nil
	}
}

// WithSecurityConfig configures the server's CA security config.
func WithSecurityConfig(securityConfig *ca.SecurityConfig) ServerOption {
	return func(s *Server) error {
		s.securityConfig = securityConfig
		return nil
	}
}

// WithNetworkDriverValidator configures the server's network driver validator.
func WithNetworkDriverValidator(validator networkallocator.DriverValidator) ServerOption {
	return func(s *Server) error {
		s.netvalidator = validator
		return nil
	}
}

// WithViewResponseMutator configures network response mutation hooks
// for the server.
//
// These hooks provide extension points for a NetworkAllocator implementation
// to hook into the Control API handlers for GetNetwork and ListNetworks, affording
// them the ability to mutate the api.Network objects loaded from the store
// before they are returned to the caller.
func WithViewResponseMutator(m ViewResponseMutator) ServerOption {
	return func(s *Server) error {
		s.viewhooks = m
		return nil
	}
}
