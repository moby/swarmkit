package node

import (
	"errors"
	"fmt"

	"github.com/moby/swarmkit/v2/api"
	"github.com/spf13/cobra"
)

var (
	activateCmd = &cobra.Command{
		Use:   "activate <node ID>",
		Short: "Activate a node",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := changeNodeAvailability(cmd, args, api.NodeAvailabilityActive); err != nil {
				if errors.Is(err, errNoChange) {
					return fmt.Errorf("Node %s is already active", args[0])
				}
				return err
			}
			return nil
		},
	}
)
