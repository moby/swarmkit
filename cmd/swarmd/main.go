package main

import (
	"os"

	"github.com/Sirupsen/logrus"
	"github.com/docker/swarm-v2/version"
	"github.com/spf13/cobra"
)

func main() {
	if err := mainCmd.Execute(); err != nil {
		logrus.Fatal(err)
	}
}

var (
	mainCmd = &cobra.Command{
		Use:   os.Args[0],
		Short: "Run a swarm control process",
		PersistentPreRun: func(cmd *cobra.Command, _ []string) {
			logrus.SetOutput(os.Stderr)
			flag, err := cmd.Flags().GetString("log-level")
			if err != nil {
				logrus.Fatal(err)
			}
			level, err := logrus.ParseLevel(flag)
			if err != nil {
				logrus.Fatal(err)
			}
			logrus.SetLevel(level)
		},
	}
)

func init() {
	mainCmd.PersistentFlags().StringP("log-level", "l", "info", "Log level (options \"debug\", \"info\", \"warn\", \"error\", \"fatal\", \"panic\")")

	mainCmd.AddCommand(
		agentCmd,
		managerCmd,
		version.Cmd,
	)
}
