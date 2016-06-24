package ping

import (
	"github.com/docker/swarmkit/api"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
)

// Ping is responsible for answering simple ping requests from other nodes.
type Ping struct{}

// Register registers the node ping server
func Register(server *grpc.Server, ping *Ping) {
	api.RegisterPingServer(server, ping)
}

// Ping is used to contact aspiring members in order to make
// sure they are reachable through their advertised address.
// Registering a member that is not reachable would potentially
// make the cluster unstable.
func (n *Ping) Ping(ctx context.Context, req *api.PingRequest) (*api.PingResponse, error) {
	return &api.PingResponse{}, nil
}
