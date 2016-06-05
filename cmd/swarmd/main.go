package main

import (
	_ "expvar"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"

	"github.com/Sirupsen/logrus"
	engineapi "github.com/docker/engine-api/client"
	"github.com/docker/swarmkit/agent"
	"github.com/docker/swarmkit/agent/exec/container"
	"github.com/docker/swarmkit/log"
	"github.com/docker/swarmkit/version"
	"github.com/spf13/cobra"
	"golang.org/x/net/context"
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

			v, err := cmd.Flags().GetBool("version")
			if err != nil {
				log.L.Fatal(err)
			}
			if v {
				version.PrintVersion()
				os.Exit(0)
			}
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			hostname, err := cmd.Flags().GetString("hostname")
			if err != nil {
				return err
			}
			addr, err := cmd.Flags().GetString("listen-addr")
			if err != nil {
				return err
			}

			advertise, err := cmd.Flags().GetString("advertise-addr")
			if err != nil {
				return err
			}

			unix, err := cmd.Flags().GetString("control-socket")
			if err != nil {
				return err
			}

			debugAddr, err := cmd.Flags().GetString("listen-debug")
			if err != nil {
				return err
			}

			managerAddr, err := cmd.Flags().GetString("join")
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

			caHash, err := cmd.Flags().GetString("ca-hash")
			if err != nil {
				return err
			}

			secret, err := cmd.Flags().GetString("secret")
			if err != nil {
				return err
			}

			engineAddr, err := cmd.Flags().GetString("engine-addr")
			if err != nil {
				return err
			}

			// todo: temporary to bypass promotion not working yet
			ismanager, err := cmd.Flags().GetBool("manager")
			if err != nil {
				return err
			}

			// Create a context for our GRPC call
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			client, err := engineapi.NewClient(engineAddr, "", nil, nil)
			if err != nil {
				return err
			}

			executor := container.NewExecutor(client)

			if debugAddr != "" {
				go func() {
					// setup listening to give access to pprof, expvar, etc.
					if err := http.ListenAndServe(debugAddr, nil); err != nil {
						panic(err)
					}
				}()
			}

			n, err := agent.NewNode(&agent.NodeConfig{
				Hostname:          hostname,
				ForceNewCluster:   forceNewCluster,
				ControlSocketPath: unix,
				ListenAddr:        addr,
				AdvertiseAddr:     advertise,
				JoinAddr:          managerAddr,
				StateDir:          stateDir,
				CAHash:            caHash,
				Secret:            secret,
				Executor:          executor,
				HeartbeatTick:     hb,
				ElectionTick:      election,
				IsManager:         ismanager,
			})
			if err != nil {
				return err
			}

			if err := n.Start(ctx); err != nil {
				return err
			}

			c := make(chan os.Signal, 1)
			signal.Notify(c, os.Interrupt)
			go func() {
				<-c
				n.Stop(ctx)
			}()

			go func() {
				<-n.Ready(ctx)
				if ctx.Err() == nil {
					logrus.Info("node is ready")
				}
			}()

			return n.Err(context.Background())
		},
	}
)

func init() {
	mainCmd.Flags().BoolP("version", "V", false, "Display the version and exit")
	mainCmd.Flags().StringP("log-level", "L", "info", "Log level (options \"debug\", \"info\", \"warn\", \"error\", \"fatal\", \"panic\")")
	mainCmd.Flags().StringP("state-dir", "d", "/var/lib/docker/swarm", "State directory")
	mainCmd.Flags().String("ca-hash", "", "Specifies the remote CA root certificate hash, necessary to join the cluster securely")
	mainCmd.Flags().String("secret", "", "Specifies the secret token required to join the cluster")
	mainCmd.Flags().String("engine-addr", "unix:///var/run/docker.sock", "Address of engine instance of agent.")
	mainCmd.Flags().String("hostname", "", "Override reported agent hostname")
	mainCmd.Flags().String("join-addr", "", "Join cluster with a node at this address")
	mainCmd.Flags().StringP("listen-addr", "l", "0.0.0.0:4242", "Listen address for peer nodes")
	mainCmd.Flags().StringP("advertise-addr", "a", "", "Address for this node to advertise to other peers")
	mainCmd.Flags().String("listen-debug", "", "Bind the Go debug server on the provided address")
	mainCmd.Flags().StringP("control-socket", "c", "/var/run/docker/swarm/docker-swarmd.sock", "Listen socket for control API")
	mainCmd.Flags().String("join", "", "Join cluster with a node at this address")
	mainCmd.Flags().Bool("force-new-cluster", false, "Force the creation of a new cluster from data directory")
	mainCmd.Flags().Uint32("heartbeat-tick", 1, "Defines the heartbeat interval (in seconds) for raft member health-check")
	mainCmd.Flags().Uint32("election-tick", 3, "Defines the amount of ticks (in seconds) needed without a Leader to trigger a new election")
	mainCmd.Flags().Bool("manager", false, "Request initial CSR in a manager role")
}
