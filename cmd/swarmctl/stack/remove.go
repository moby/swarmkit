package stack

import (
	"errors"

	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/cmd/swarmctl/common"
	"github.com/spf13/cobra"
)

var (
	removeCmd = &cobra.Command{
		Use:     "remove <stack>",
		Short:   "Remove a a stack",
		Aliases: []string{"rm"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return errors.New("stack name missing")
			}

			c, err := common.Dial(cmd)
			if err != nil {
				return err
			}

			r, err := c.ListServices(common.Context(cmd), &api.ListServicesRequest{})
			if err != nil {
				return err
			}

			for _, j := range r.Services {
				if j.Spec.Annotations.Labels["stack"] == args[0] {
					_, err = c.RemoveService(common.Context(cmd), &api.RemoveServiceRequest{ServiceID: j.ID})
					if err != nil {
						return err
					}
				}
			}

			return nil
		},
	}
)
