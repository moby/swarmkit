package service

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/docker/swarmkit/api"
	"github.com/docker/swarmkit/cmd/swarmctl/common"
	"github.com/spf13/cobra"
)

var (
	removeReplicaCmd = &cobra.Command{
		Use:     "remove-replica <task name>",
		Short:   "Remove a replica referenced by <task name>",
		Aliases: []string{"rm-replica"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return errors.New("task name missing")
			}

			if len(args) > 1 {
				return errors.New("remove-replica command takes exactly 1 argument")
			}

			var parts []string
			if parts = strings.SplitN(args[0], ".", 2); len(parts) != 2 {
				return errors.New("Invalid task name")
			}
			serviceID, slotStr := parts[0], parts[1]

			c, err := common.Dial(cmd)
			if err != nil {
				return err
			}

			service, err := getService(common.Context(cmd), c, serviceID)
			if err != nil {
				return err
			}

			_, ok := service.Spec.GetMode().(*api.ServiceSpec_Replicated)
			if !ok {
				return errors.New("remove-replica can only be used with replicated mode")
			}

			slot, err := strconv.ParseUint(slotStr, 10, 64)
			if err != nil {
				return errors.New("Invalid slot value")
			}

			_, err = c.RemoveReplica(common.Context(cmd), &api.RemoveReplicaRequest{ServiceID: service.ID, Slot: slot})
			if err != nil {
				return err
			}
			fmt.Println(args[0])
			return nil
		},
	}
)
