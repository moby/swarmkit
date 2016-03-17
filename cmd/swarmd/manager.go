package main

import (
	"github.com/docker/swarm-v2/ca"
	"github.com/docker/swarm-v2/manager"
	"github.com/spf13/cobra"
	"golang.org/x/net/context"
)

var managerCmd = &cobra.Command{
	Use:   "manager",
	Short: "Run the swarm manager",
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := context.Background()
		addr, err := cmd.Flags().GetString("listen-addr")
		if err != nil {
			return err
		}

		managerAddr, err := cmd.Flags().GetString("join-cluster")
		if err != nil {
			return err
		}

		stateDir, err := cmd.Flags().GetString("state-dir")
		if err != nil {
			return err
		}

		token, err := cmd.Flags().GetString("token")
		if err != nil {
			return err
		}

		securityConfig, err := ca.LoadOrCreateManagerSecurityConfig(stateDir, token, managerAddr, false)
		if err != nil {
			return err
		}

		m, err := manager.New(&manager.Config{
			ListenProto:    "tcp",
			SecurityConfig: securityConfig,
			ListenAddr:     addr,
			JoinRaft:       managerAddr,
			StateDir:       stateDir,
		})
		if err != nil {
			return err
		}
		return m.Run(ctx)
	},
}

func init() {
	managerCmd.Flags().String("listen-addr", "0.0.0.0:4242", "Listen address")
	managerCmd.Flags().String("join-cluster", "", "Join cluster with a node at this address")
}
