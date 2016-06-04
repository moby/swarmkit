package main

import (
	"os"

	"github.com/Sirupsen/logrus"
	"github.com/docker/swarm-v2/log"
	"github.com/docker/swarm-v2/version"
	"github.com/spf13/cobra"
)

func main() {
	if err := mainCmd.Execute(); err != nil {
		log.L.Fatal(err)
	}
}

var (
	mainCmd = &cobra.Command{
		Use:          os.Args[0],
		Short:        "Run a swarm control process",
		SilenceUsage: true,
		PersistentPreRun: func(cmd *cobra.Command, _ []string) {
			logrus.SetOutput(os.Stderr)
			flag, err := cmd.Flags().GetString("log-level")
			if err != nil {
				log.L.Fatal(err)
			}
			level, err := logrus.ParseLevel(flag)
			if err != nil {
				log.L.Fatal(err)
			}
			logrus.SetLevel(level)
		},
	}
)

func init() {
	mainCmd.PersistentFlags().StringP("log-level", "l", "info", "Log level (options \"debug\", \"info\", \"warn\", \"error\", \"fatal\", \"panic\")")
	mainCmd.PersistentFlags().StringP("state-dir", "d", "/var/lib/docker/cluster", "State directory")
	mainCmd.PersistentFlags().StringP("ca-hash", "c", "", "Specifies the remote CA root certificate hash, necessary to join the cluster securely")
	mainCmd.PersistentFlags().StringP("secret", "s", "", "Specifies the secret token required to join the cluster")

	mainCmd.AddCommand(
		agentCmd,
		managerCmd,
		nodeCmd,
		version.Cmd,
	)
}
