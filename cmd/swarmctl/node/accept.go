package node

import (
	"fmt"

	"github.com/docker/swarm-v2/api"
	"github.com/spf13/cobra"
)

var (
	acceptCmd = &cobra.Command{
		Use:   "accept <node ID>",
		Short: "Accept a node into the cluster",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := changeNodeMembership(cmd, args, api.NodeMembershipAccepted); err != nil {
				if err == errNoChange {
					return fmt.Errorf("Node %s was already accepted", args[0])
				}
				return err
			}
			return nil
		},
	}
)
