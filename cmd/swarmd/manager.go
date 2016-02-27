package main

import (
	"github.com/docker/swarm-v2/manager"
	"github.com/docker/swarm-v2/state"
	"github.com/spf13/cobra"
)

var managerCmd = &cobra.Command{
	Use:   "manager",
	Short: "Run the swarm manager",
	RunE: func(cmd *cobra.Command, args []string) error {
		addr, err := cmd.Flags().GetString("listen-addr")
		if err != nil {
			return err
		}

		store := state.NewMemoryStore()

		m := manager.New(&manager.Config{
			Store:       store,
			ListenProto: "tcp",
			ListenAddr:  addr,
		})
		return m.ListenAndServe()
	},
}

func init() {
	managerCmd.Flags().String("listen-addr", "0.0.0.0:4242", "Listen address")
}
