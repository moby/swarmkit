package state

import (
	"time"

	"github.com/docker/swarm-v2/api"

	"google.golang.org/grpc"
)

const (
	// MaxRetries is the maximum number of retries allowed to
	// initiate a grpc connection to a remote raft member
	MaxRetries = 3
)

// Raft represents a connection to a raft member
type Raft struct {
	api.RaftClient
	Conn *grpc.ClientConn
}

// GetRaftClient returns a raft client object to communicate
// with other raft members
func GetRaftClient(addr string, timeout time.Duration) (*Raft, error) {
	conn, err := dial(addr, "tcp", timeout)
	if err != nil {
		return nil, err
	}

	return &Raft{
		RaftClient: api.NewRaftClient(conn),
		Conn:       conn,
	}, nil
}

// dial returns a grpc client connection
func dial(addr string, protocol string, timeout time.Duration) (*grpc.ClientConn, error) {
	conn, err := grpc.Dial(addr, grpc.WithInsecure(), grpc.WithTimeout(timeout))
	if err != nil {
		return nil, err
	}

	return conn, nil
}

// Register registers the node raft server
func Register(server *grpc.Server, node *Node) {
	api.RegisterRaftServer(server, node)
}
