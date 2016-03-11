package common

import (
	"time"

	"github.com/docker/swarm-v2/api"
	"github.com/spf13/cobra"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
)

// Dial establishes a connection and creates a client.
// It infers connection parameters from CLI options.
func Dial(cmd *cobra.Command) (api.ClusterClient, error) {
	addr, err := cmd.Flags().GetString("addr")
	if err != nil {
		return nil, err
	}

	conn, err := grpc.Dial(addr, grpc.WithBlock(), grpc.WithInsecure(), grpc.WithTimeout(time.Second))
	if err != nil {
		return nil, err
	}

	client := api.NewClusterClient(conn)
	return client, nil
}

// Context returns a request context based on CLI arguments.
func Context(cmd *cobra.Command) context.Context {
	// TODO(aluzzardi): Actually create a context.
	return context.TODO()
}
