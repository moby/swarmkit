package state

import (
	"time"

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
	backoffConfig := *grpc.DefaultBackoffConfig
	backoffConfig.MaxDelay = 2 * time.Second

	grpcOptions := []grpc.DialOption{
		grpc.WithBackoffConfig(&backoffConfig),
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
