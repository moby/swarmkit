package main

import (
	"fmt"
	"os"

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
	mainCmd.AddCommand(version.Cmd)
}
