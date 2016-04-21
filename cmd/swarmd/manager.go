package main

import (
	"os"
	"os/signal"

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

		unix, err := cmd.Flags().GetString("listen-unix")
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

		// Create a context for our GRPC call
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		securityConfig, err := ca.LoadOrCreateManagerSecurityConfig(ctx, stateDir, token, managerAddr, false)
		if err != nil {
			return err
		}

		m, err := manager.New(&manager.Config{
			ProtoAddr: map[string]string{
				"tcp":  addr,
				"unix": unix,
			},
			SecurityConfig: securityConfig,
			JoinRaft:       managerAddr,
			StateDir:       stateDir,
		})
		if err != nil {
			return err
		}

		c := make(chan os.Signal, 1)
		signal.Notify(c, os.Interrupt)
		go func() {
			<-c
			m.Stop(ctx)
		}()

		return m.Run(ctx)
	},
}

func init() {
	managerCmd.Flags().String("listen-addr", "0.0.0.0:4242", "Listen address")
	managerCmd.Flags().String("listen-unix", "/var/run/docker/cluster/docker-swarmd.sock", "Listen socket")
	managerCmd.Flags().String("join-cluster", "", "Join cluster with a node at this address")
}
