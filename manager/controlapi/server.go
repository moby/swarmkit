package controlapi

import (
	"errors"

	"github.com/docker/docker/pkg/plugingetter"
	"github.com/docker/swarmkit/ca"
	"github.com/docker/swarmkit/manager/drivers"
	"github.com/docker/swarmkit/manager/state/raft"
	"github.com/docker/swarmkit/manager/state/store"
)

var (
	errInvalidArgument = errors.New("invalid argument")
)

// Server is the Cluster API gRPC server.

type ServerTaskExecMessage struct {
	Message []byte
}
type ServerTaskExecChannels struct {
	in  map[string]chan ServerTaskExecMessage
	out map[string]chan ServerTaskExecMessage
}

func NewServerTaskExecChannels() *ServerTaskExecChannels {
	return &ServerTaskExecChannels{
		in:  make(map[string]chan ServerTaskExecMessage),
		out: make(map[string]chan ServerTaskExecMessage),
	}
}

func (s *ServerTaskExecChannels) registerExecChannels(containerid string) {
	if _, ok := s.in[containerid]; !ok {
		s.in[containerid] = make(chan ServerTaskExecMessage)
	}
	if _, ok := s.out[containerid]; !ok {
		s.out[containerid] = make(chan ServerTaskExecMessage)
	}
}

func (s *ServerTaskExecChannels) In(containerid string) chan ServerTaskExecMessage {
	s.registerExecChannels(containerid)
	return s.in[containerid]
}

func (s *ServerTaskExecChannels) Out(containerid string) chan ServerTaskExecMessage {
	s.registerExecChannels(containerid)
	return s.out[containerid]
}

func (s *ServerTaskExecChannels) Outs() map[string]chan ServerTaskExecMessage {
	return s.out
}

func (s *ServerTaskExecChannels) Ins() map[string]chan ServerTaskExecMessage {
	return s.in
}

type Server struct {
	store          *store.MemoryStore
	raft           *raft.Node
	securityConfig *ca.SecurityConfig
	pg             plugingetter.PluginGetter
	dr             *drivers.DriverProvider
	taskExecChs    *ServerTaskExecChannels
}

// NewServer creates a Cluster API server.
func NewServer(store *store.MemoryStore, raft *raft.Node, securityConfig *ca.SecurityConfig, pg plugingetter.PluginGetter, dr *drivers.DriverProvider, channels *ServerTaskExecChannels) *Server {
	return &Server{
		store:          store,
		dr:             dr,
		raft:           raft,
		securityConfig: securityConfig,
		pg:             pg,
		taskExecChs:    channels,
	}
}
