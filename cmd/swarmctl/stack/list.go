package stack

import (
	"fmt"

	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/cmd/swarmctl/common"
	"github.com/spf13/cobra"
)

var (
	listCmd = &cobra.Command{
		Use:   "ls",
		Short: "List stacks",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := common.Dial(cmd)
			if err != nil {
				return err
			}

			r, err := c.ListServices(common.Context(cmd), &api.ListServicesRequest{})
			if err != nil {
				return err
			}

			stacks := map[string]struct{}{}

			for _, j := range r.Services {
				stack := j.Spec.Annotations.Labels["stack"]
				if stack != "" {
					stacks[stack] = struct{}{}
				}
			}

			for stack, _ := range stacks {
				fmt.Println(stack)
			}

			return nil
		},
	}
)
