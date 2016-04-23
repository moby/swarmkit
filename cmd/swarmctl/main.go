package main

import (
	"os"

	"github.com/docker/swarm-v2/cmd/swarmctl/managers"
	"github.com/docker/swarm-v2/cmd/swarmctl/network"
	"github.com/docker/swarm-v2/cmd/swarmctl/node"
	"github.com/docker/swarm-v2/cmd/swarmctl/root"
	"github.com/docker/swarm-v2/cmd/swarmctl/service"
	"github.com/docker/swarm-v2/cmd/swarmctl/task"
	"github.com/docker/swarm-v2/cmd/swarmctl/volume"
	"github.com/docker/swarm-v2/version"
	"github.com/spf13/cobra"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
)

func main() {
	if c, err := mainCmd.ExecuteC(); err != nil {
		c.Println("Error:", grpc.ErrorDesc(err))
		// if it's not a grpc, we assume it's a user error and we display the usage.
		if grpc.Code(err) == codes.Unknown {
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
	mainCmd.PersistentFlags().StringP("host", "H", "127.0.0.1:4242", "Specify the address of the manager to connect to")
	mainCmd.PersistentFlags().BoolP("no-resolve", "n", false, "Do not try to map IDs to Names when displaying them")

	mainCmd.AddCommand(root.Cmds...)

	mainCmd.AddCommand(
		node.Cmd,
		service.Cmd,
		task.Cmd,
		volume.Cmd,
		version.Cmd,
		network.Cmd,
		managers.Cmd,
	)
}
