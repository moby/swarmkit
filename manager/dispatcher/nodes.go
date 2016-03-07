package dispatcher

import (
	"sync"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"

	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/pkg/heartbeat"
)

type registeredNode struct {
	SessionID string
	Heartbeat *heartbeat.Heartbeat
	Tasks     []string
	Node      *api.Node
	mu        sync.Mutex
}

// checkSessionID determines if the SessionID has changed and returns the
// appropriate GRPC error code.
//
// This may not belong here in the future.
func (rn *registeredNode) checkSessionID(sessionID string) error {
	rn.mu.Lock()
	defer rn.mu.Unlock()

	// Before each message send, we need to check the nodes sessionID hasn't
	// changed. If it has, we will the stream and make the node
	// re-register.
	if rn.SessionID != sessionID {
		return grpc.Errorf(codes.InvalidArgument, ErrSessionInvalid.Error())
	}

	return nil
}

type nodeStore struct {
	nodes map[string]*registeredNode
	mu    sync.RWMutex
}

func newNodeStore() *nodeStore {
	return &nodeStore{
		nodes: make(map[string]*registeredNode),
	}
}

// Add adds new node, it replaces existing without notification.
func (s *nodeStore) Add(rn *registeredNode) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if existRn, ok := s.nodes[rn.Node.ID]; ok {
		existRn.Heartbeat.Stop()
		delete(s.nodes, rn.Node.ID)
	}
	s.nodes[rn.Node.ID] = rn
}

func (s *nodeStore) Get(id string) (*registeredNode, error) {
	s.mu.RLock()
	rn, ok := s.nodes[id]
	s.mu.RUnlock()
	if !ok {
		return nil, grpc.Errorf(codes.NotFound, ErrNodeNotRegistered.Error())
	}
	return rn, nil
}

func (s *nodeStore) GetWithSession(id, sid string) (*registeredNode, error) {
	s.mu.RLock()
	rn, ok := s.nodes[id]
	s.mu.RUnlock()
	if !ok {
		return nil, grpc.Errorf(codes.NotFound, ErrNodeNotRegistered.Error())
	}
	return rn, rn.checkSessionID(sid)
}

func (s *nodeStore) Delete(id string) {
	s.mu.Lock()
	if rn, ok := s.nodes[id]; ok {
		delete(s.nodes, id)
		rn.Heartbeat.Stop()
	}
	s.mu.Unlock()
}
