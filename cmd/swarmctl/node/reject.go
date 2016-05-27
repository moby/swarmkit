package node

import (
	"fmt"

	"github.com/docker/swarm-v2/api"
	"github.com/spf13/cobra"
)

var (
	rejectCmd = &cobra.Command{
		Use:   "reject <node ID>",
		Short: "Block a node's admission into the cluster",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := changeNodeAcceptance(cmd, args, api.NodeAcceptanceReject); err != nil {
				if err == errNoChange {
					return fmt.Errorf("Node %s was already rejected", args[0])
				}
				return err
			}
			return nil
		},
	}
)
