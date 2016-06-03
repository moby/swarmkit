package main

import (
	"path/filepath"
	"time"

	engineapi "github.com/docker/engine-api/client"
	"github.com/docker/swarm-v2/agent"
	"github.com/docker/swarm-v2/agent/exec/container"
	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/ca"
	"github.com/docker/swarm-v2/log"
	"github.com/docker/swarm-v2/picker"
	"github.com/spf13/cobra"
	"golang.org/x/net/context"
)

var (
	agentCmd = &cobra.Command{
		Use:   "agent",
		Short: "Run the swarm agent",
		Long: `Start a swarm agent with the provided path. If starting from an
empty path, the agent will allocate an identity and startup. If data is
already present, the agent will recover and startup.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			hostname, err := cmd.Flags().GetString("hostname")
			if err != nil {
				return err
			}

			managerAddrs, err := cmd.Flags().GetStringSlice("manager")
			if err != nil {
				return err
			}

			engineAddr, err := cmd.Flags().GetString("engine-addr")
			if err != nil {
				return err
			}

			log.G(ctx).Debugf("managers: %v", managerAddrs)
			stateDir, err := cmd.Flags().GetString("state-dir")
			if err != nil {
				return err
			}

			certDir := filepath.Join(stateDir, "certificates")

			token, err := cmd.Flags().GetString("token")
			if err != nil {
				return err
			}

			peers := make([]api.Peer, 0, len(managerAddrs))
			for _, a := range managerAddrs {
				peers = append(peers, api.Peer{Addr: a})
			}

			managers := picker.NewRemotes(peers...)
			picker := picker.NewPicker(managers)

			ctx, cancel := context.WithCancel(ctx)
			defer cancel()

			securityConfig, err := ca.LoadOrCreateSecurityConfig(ctx, certDir, token, ca.AgentRole, picker)
			if err != nil {
				return err
			}

			renew := make(chan struct{}, 1)
			updates := ca.RenewTLSConfig(ctx, securityConfig, certDir, picker, 30*time.Second, renew)
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

			client, err := engineapi.NewClient(engineAddr, "", nil, nil)
			if err != nil {
				return err
			}

			executor := container.NewExecutor(client)

			ag, err := agent.New(&agent.Config{
				Hostname: hostname,
				Managers: managers,
				Executor: executor,
			})
			if err != nil {
				log.G(ctx).Fatalln(err)
			}

			if err := ag.Start(ctx); err != nil {
				return err
			}

			// TODO(stevvooe): Register signal to gracefully shutdown agent.

			return ag.Err(context.Background())
		},
	}
)

func init() {
	agentCmd.Flags().String("engine-addr", "unix:///var/run/docker.sock", "Address of engine instance of agent.")
	agentCmd.Flags().String("hostname", "", "Override reported agent hostname")
	agentCmd.Flags().StringSliceP("manager", "m", []string{"localhost:4242"}, "Specify one or more manager addresses")
}
