package managers

import (
	"errors"
	"fmt"

	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/cmd/swarmctl/common"
	"github.com/spf13/cobra"
)

var (
	removeCmd = &cobra.Command{
		Use:   "rm",
		Short: "Remove a manager",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return errors.New("manager ID missing")
			}

			c, err := common.Dial(cmd)
			if err != nil {
				return err
			}
			_, err = c.RemoveManager(common.Context(cmd), &api.RemoveManagerRequest{ManagerID: args[0]})
			if err != nil {
				return err
			}

			fmt.Printf("Member %s successfully removed from the cluster\n", args[0])
			return nil
		},
	}
)
