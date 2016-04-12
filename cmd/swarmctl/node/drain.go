package node

import (
	"fmt"

	specspb "github.com/docker/swarm-v2/pb/docker/cluster/specs"
	"github.com/spf13/cobra"
)

var (
	drainCmd = &cobra.Command{
		Use:   "drain <node ID>",
		Short: "Drain a node",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := changeNodeAvailability(cmd, args, specspb.NodeAvailabilityDrain); err != nil {
				if err == errNoChange {
					return fmt.Errorf("Node %s was already drained", args[0])
				}
				return err
			}
			return nil
		},
	}
)
