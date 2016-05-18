package main

import (
	"fmt"
	"net"
	"path/filepath"

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
		addrHost, _, err := net.SplitHostPort(addr)
		if err == nil {
			ip := net.ParseIP(addrHost)
			if ip != nil && (ip.IsUnspecified() || ip.IsLoopback()) {
				fmt.Println("Warning: Specifying a valid address with --listen-addr may be necessary for other managers to reach this one.")
			}
		}

		managerAddr, err := cmd.Flags().GetString("join-cluster")
		if err != nil {
			return err
		}

		forceNewCluster, err := cmd.Flags().GetBool("force-new-cluster")
		if err != nil {
			return err
		}

		stateDir, err := cmd.Flags().GetString("state-dir")
		if err != nil {
			return err
		}

		certDir := filepath.Join(stateDir, "certificates")

		token, err := cmd.Flags().GetString("token")
		if err != nil {
			return err
		}

		// Create a context for our GRPC call
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		securityConfig, err := ca.LoadOrCreateManagerSecurityConfig(ctx, certDir, token, managerAddr)
		if err != nil {
			return err
		}

		m, err := manager.New(&manager.Config{
			ListenProto:     "tcp",
			SecurityConfig:  securityConfig,
			ListenAddr:      addr,
			ForceNewCluster: forceNewCluster,
			JoinRaft:        managerAddr,
			StateDir:        stateDir,
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
	managerCmd.Flags().Bool("force-new-cluster", false, "Force the creation of a new cluster from data directory")
}
