package main

import (
	"fmt"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/docker/swarm-v2/ca"
	"github.com/docker/swarm-v2/manager"
	"github.com/docker/swarm-v2/picker"
	"github.com/spf13/cobra"
	"golang.org/x/net/context"
)

var managerCmd = &cobra.Command{
	Use:   "manager",
	Short: "Run the swarm manager",
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := context.Background()
		addr, err := cmd.Flags().GetString("listen-remote-api")
		if err != nil {
			return err
		}
		addrHost, _, err := net.SplitHostPort(addr)
		if err == nil {
			ip := net.ParseIP(addrHost)
			if ip != nil && (ip.IsUnspecified() || ip.IsLoopback()) {
				fmt.Println("Warning: Specifying a valid address with --listen-remote-api may be necessary for other managers to reach this one.")
			}
		}

		unix, err := cmd.Flags().GetString("listen-control-api")
		if err != nil {
			return err
		}

		managerAddr, err := cmd.Flags().GetString("join-cluster")
		if err != nil {
			return err
		}

		forceNewCluster, err := cmd.Flags().GetBool("force-new-cluster")
		if err != nil {
			return err
		}

		hb, err := cmd.Flags().GetUint32("heartbeat-tick")
		if err != nil {
			return err
		}

		election, err := cmd.Flags().GetUint32("election-tick")
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

		var p *picker.Picker
		if managerAddr != "" {
			managers := picker.NewRemotes(managerAddr)
			p = picker.NewPicker(managerAddr, managers)
		} else {
			_, err := ca.GetLocalRootCA(certDir)
			if err != nil {
				// If we are not provided a valid join address and there is no local valid Root CA
				// we should bootstrap a new cluster
				if err := ca.BootstrapCluster(certDir); err != nil {
					return err
				}
			}
			managers := picker.NewRemotes(addr)
			p = picker.NewPicker(addr, managers)
		}

		// We either just boostraped our cluster from scratch, or have a valid picker and
		// are thus joining an existing cluster
		securityConfig, err := ca.LoadOrCreateSecurityConfig(ctx, certDir, token, ca.ManagerRole, p)
		if err != nil {
			return err
		}

		updates := ca.RenewTLSConfig(ctx, securityConfig, certDir, p, 30*time.Second)
		go func() {
			for {
				select {
				case certUpdate := <-updates:
					if certUpdate.Err != nil {
						continue
					}
				case <-ctx.Done():
					break
				}
			}
		}()

		m, err := manager.New(&manager.Config{
			ForceNewCluster: forceNewCluster,
			ProtoAddr: map[string]string{
				"tcp":  addr,
				"unix": unix,
			},
			SecurityConfig: securityConfig,
			JoinRaft:       managerAddr,
			StateDir:       stateDir,
			HeartbeatTick:  hb,
			ElectionTick:   election,
		})
		if err != nil {
			return err
		}

		c := make(chan os.Signal, 1)
		signal.Notify(c, os.Interrupt, syscall.SIGTERM)
		go func() {
			<-c
			m.Stop(ctx)
		}()

		return m.Run(ctx)
	},
}

func init() {
	managerCmd.Flags().String("listen-remote-api", "0.0.0.0:4242", "Listen address for remote API")
	managerCmd.Flags().String("listen-control-api", "/var/run/docker/cluster/docker-swarmd.sock", "Listen socket for control API")
	managerCmd.Flags().String("join-cluster", "", "Join cluster with a node at this address")
	managerCmd.Flags().Bool("force-new-cluster", false, "Force the creation of a new cluster from data directory")
	managerCmd.Flags().Uint32("heartbeat-tick", 1, "Defines the heartbeat interval (in seconds) for raft member health-check")
	managerCmd.Flags().Uint32("election-tick", 3, "Defines the amount of ticks (in seconds) needed without a Leader to trigger a new election")
}
