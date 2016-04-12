package node

import (
	"fmt"

	specspb "github.com/docker/swarm-v2/pb/docker/cluster/specs"
	"github.com/spf13/cobra"
)

var (
	activateCmd = &cobra.Command{
		Use:   "activate <node ID>",
		Short: "Activate a node",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := changeNodeAvailability(cmd, args, specspb.NodeAvailabilityActive); err != nil {
				if err == errNoChange {
					return fmt.Errorf("Node %s is already active", args[0])
				}
				return err
			}
			return nil
		},
	}
)
