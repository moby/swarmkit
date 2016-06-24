package common

import (
	"crypto/tls"
	"net"
	"time"

	"github.com/docker/swarmkit/api"
	"github.com/spf13/cobra"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

// Dial establishes a connection and creates a client.
// It infers connection parameters from CLI options.
func Dial(cmd *cobra.Command) (api.ControlClient, error) {
	addr, err := cmd.Flags().GetString("socket")
	if err != nil {
		return nil, err
	}

	opts := []grpc.DialOption{}
	insecureCreds := credentials.NewTLS(&tls.Config{InsecureSkipVerify: true})
	opts = append(opts, grpc.WithTransportCredentials(insecureCreds))
	opts = append(opts, grpc.WithDialer(
		func(addr string, timeout time.Duration) (net.Conn, error) {
			return net.DialTimeout("unix", addr, timeout)
		}))
	conn, err := grpc.Dial(addr, opts...)
	if err != nil {
		return nil, err
	}

	client := api.NewControlClient(conn)
	return client, nil
}

// Context returns a request context based on CLI arguments.
func Context(cmd *cobra.Command) context.Context {
	// TODO(aluzzardi): Actually create a context.
	return context.TODO()
}
