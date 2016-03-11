package main

import (
	"os"
	"strings"

	"github.com/docker/swarm-v2/cmd/swarmctl/job"
	"github.com/docker/swarm-v2/cmd/swarmctl/node"
	"github.com/docker/swarm-v2/cmd/swarmctl/task"
	"github.com/docker/swarm-v2/version"
	"github.com/spf13/cobra"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
)

func main() {
	if c, err := mainCmd.ExecuteC(); err != nil {
		c.Println("Error:", grpc.ErrorDesc(err))
		// if it's not a grpc, we assume it's a user error and we display the usage.
		if !strings.HasPrefix(grpc.ErrorDesc(err), "grpc:") && grpc.Code(err) == codes.Unknown {
			c.Println(c.UsageString())
		}

		os.Exit(-1)
	}
}

var (
	mainCmd = &cobra.Command{
		Use:           os.Args[0],
		Short:         "Control a swarm cluster",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
)

func init() {
	mainCmd.PersistentFlags().StringP("addr", "a", "127.0.0.1:4242", "Address of the Swarm manager")

	mainCmd.AddCommand(
		node.Cmd,
		job.Cmd,
		task.Cmd,
		version.Cmd,
	)
}
