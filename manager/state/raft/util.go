package raft

import (
	"time"

	"golang.org/x/net/context"

	"github.com/docker/swarm-v2/api"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

// Raft represents a connection to a raft member
type Raft struct {
	api.RaftClient
	Conn *grpc.ClientConn
}

// dial returns a grpc client connection
func dial(addr string, protocol string, creds credentials.TransportAuthenticator, timeout time.Duration) (*grpc.ClientConn, error) {
	grpcOptions := []grpc.DialOption{
		grpc.WithBackoffMaxDelay(2 * time.Second),
		grpc.WithTransportCredentials(creds),
	}

	if timeout != 0 {
		grpcOptions = append(grpcOptions, grpc.WithTimeout(timeout))
	}

	return grpc.Dial(addr, grpcOptions...)
}

// Register registers the node raft server
func Register(server *grpc.Server, node *Node) {
	api.RegisterRaftServer(server, node)
}

// WaitForLeader waits until node observe some leader in cluster. It returns
// error if ctx was cancelled before leader appeared.
func WaitForLeader(ctx context.Context, n *Node) error {
	l := n.Leader()
	if l != 0 {
		return nil
	}
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for l == 0 {
		select {
		case <-ticker.C:
		case <-ctx.Done():
			return ctx.Err()
		}
		l = n.Leader()
	}
	return nil
}
