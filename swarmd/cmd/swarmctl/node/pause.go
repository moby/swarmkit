package node

import (
	"errors"
	"fmt"

	"github.com/moby/swarmkit/v2/api"
	"github.com/spf13/cobra"
)

var (
	pauseCmd = &cobra.Command{
		Use:   "pause <node ID>",
		Short: "Pause a node",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := changeNodeAvailability(cmd, args, api.NodeAvailabilityPause); err != nil {
				if errors.Is(err, errNoChange) {
					return fmt.Errorf("Node %s was already paused", args[0])
				}
				return err
			}
			return nil
		},
	}
)
