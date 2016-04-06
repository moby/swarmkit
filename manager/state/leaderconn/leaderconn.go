package leaderconn

import (
	"errors"

	"google.golang.org/grpc"
)

// ConnSelector is interface for obtaining connection to remote leader.
// ErrLocalLeader must be returned if current node is a leader.
type ConnSelector interface {
	LeaderConn() (*grpc.ClientConn, error)
}

// ErrLocalLeader returned from LeaderConn when local instance should be used.
var ErrLocalLeader = errors.New("curent node is leader")
