package main

import (
	"fmt"
	"os"

	// TODO(abronan): remove these blank imports after
	// including the raft backend
	_ "github.com/coreos/etcd/raft"
	_ "github.com/coreos/etcd/raft/raftpb"
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
		Short: "Run a swarm control process",
	}
)

func init() {
	mainCmd.AddCommand(version.Cmd)
	mainCmd.AddCommand(managerCmd)
}
