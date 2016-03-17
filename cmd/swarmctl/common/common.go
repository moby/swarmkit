package common

import (
	"crypto/tls"

	"github.com/docker/swarm-v2/api"
	"github.com/spf13/cobra"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

// Dial establishes a connection and creates a client.
// It infers connection parameters from CLI options.
func Dial(cmd *cobra.Command) (api.ClusterClient, error) {
	addr, err := cmd.Flags().GetString("addr")
	if err != nil {
		return nil, err
	}

	// TODO(diogo): this can't be an insecure connection. Either using CA cert,
	// TOFU, or local unix socket
	opts := []grpc.DialOption{}
	insecureCreds := credentials.NewTLS(&tls.Config{InsecureSkipVerify: true})
	opts = append(opts, grpc.WithTransportCredentials(insecureCreds))

	conn, err := grpc.Dial(addr, opts...)
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
