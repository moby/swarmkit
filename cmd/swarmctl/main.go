package main

import (
	"os"

	"github.com/moby/swarmkit/v2/cmd/swarmctl/cluster"
	"github.com/moby/swarmkit/v2/cmd/swarmctl/config"
	"github.com/moby/swarmkit/v2/cmd/swarmctl/network"
	"github.com/moby/swarmkit/v2/cmd/swarmctl/node"
	"github.com/moby/swarmkit/v2/cmd/swarmctl/secret"
	"github.com/moby/swarmkit/v2/cmd/swarmctl/service"
	"github.com/moby/swarmkit/v2/cmd/swarmctl/task"
	"github.com/moby/swarmkit/v2/cmd/swarmd/defaults"
	"github.com/moby/swarmkit/v2/version"
	"github.com/spf13/cobra"
	"google.golang.org/grpc/status"
)

func main() {
	if c, err := mainCmd.ExecuteC(); err != nil {
		s, _ := status.FromError(err)
		c.Println("Error:", s.Message())
		// if it's not a grpc, we assume it's a user error and we display the usage.
		if _, ok := status.FromError(err); !ok {
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

func defaultSocket() string {
	swarmSocket := os.Getenv("SWARM_SOCKET")
	if swarmSocket != "" {
		return swarmSocket
	}
	return defaults.ControlAPISocket
}

func init() {
	mainCmd.PersistentFlags().StringP("socket", "s", defaultSocket(), "Socket to connect to the Swarm manager")
	mainCmd.PersistentFlags().BoolP("no-resolve", "n", false, "Do not try to map IDs to Names when displaying them")

	mainCmd.AddCommand(
		node.Cmd,
		service.Cmd,
		task.Cmd,
		version.Cmd,
		network.Cmd,
		cluster.Cmd,
		secret.Cmd,
		config.Cmd,
	)
}
