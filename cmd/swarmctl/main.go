package main

import (
	"fmt"
	"os"

	"github.com/docker/swarm-v2/cmd/swarmctl/job"
	"github.com/docker/swarm-v2/cmd/swarmctl/node"
	"github.com/docker/swarm-v2/cmd/swarmctl/task"
	"github.com/docker/swarm-v2/version"

	"github.com/spf13/cobra"
)

func main() {
	if err := mainCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
}

var (
	mainCmd = &cobra.Command{
		Use:   os.Args[0],
		Short: "Control a swarm cluster",
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
