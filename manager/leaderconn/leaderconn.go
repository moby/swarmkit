package leaderconn

import (
	"errors"

	"github.com/docker/swarm-v2/manager/state"
	"google.golang.org/grpc"
)

// ErrLocalLeader is returned when leader is local machine.
var ErrLocalLeader = errors.New("local instance is leader")

// Getter should return connection to "cluster leader", where requests
// for updates will be sent. It should return ErrLocalLeader if local storage
// must be used.
type Getter interface {
	LeaderConn() (*grpc.ClientConn, error)
}

// NewRaftLeaderConnGetter creates Getter which returs leader of raft cluster.
func NewRaftLeaderConnGetter(node *state.Node) *RaftLeaderConnGetter {
	return &RaftLeaderConnGetter{
		node: node,
	}
}

// RaftLeaderConnGetter is Getter for raft cluster.
type RaftLeaderConnGetter struct {
	node *state.Node
}

// LeaderConn returns grpc connection to raft cluster leader or ErrLocalLeader
// error if current node is a leader.
func (rl *RaftLeaderConnGetter) LeaderConn() (*grpc.ClientConn, error) {
	peer := rl.node.Cluster.Peers()[rl.node.Leader()]
	if peer.Client == nil {
		return nil, ErrLocalLeader
	}
	return peer.Client.Conn, nil
}
