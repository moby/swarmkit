package node

import (
	"fmt"

	specspb "github.com/docker/swarm-v2/pb/docker/cluster/specs"
	"github.com/spf13/cobra"
)

var (
	pauseCmd = &cobra.Command{
		Use:   "pause <node ID>",
		Short: "Pause a node",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := changeNodeAvailability(cmd, args, specspb.NodeAvailabilityPause); err != nil {
				if err == errNoChange {
					return fmt.Errorf("Node %s was already paused", args[0])
				}
				return err
			}
			return nil
		},
	}
)
