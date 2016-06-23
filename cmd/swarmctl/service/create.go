package service

import (
	"errors"
	"fmt"

	"github.com/docker/swarmkit/api"
	"github.com/docker/swarmkit/cmd/swarmctl/common"
	"github.com/docker/swarmkit/cmd/swarmctl/service/flagparser"
	"github.com/spf13/cobra"
)

var (
	createCmd = &cobra.Command{
		Use:   "create [OPTIONS] IMAGE [COMMAND] [ARG...]",
		Short: "Create a service",
		RunE: func(cmd *cobra.Command, args []string) error {
			if !cmd.Flags().Changed("name") {
				return errors.New("--name is mandatory")
			}
			if len(args) < 1 {
				return errors.New("IMAGE is required")
			}

			c, err := common.Dial(cmd)
			if err != nil {
				return err
			}

			spec := &api.ServiceSpec{
				Mode: &api.ServiceSpec_Replicated{
					Replicated: &api.ReplicatedService{
						Replicas: 1,
					},
				},
				Task: api.TaskSpec{
					Runtime: &api.TaskSpec_Container{
						Container: &api.ContainerSpec{},
					},
				},
			}

			if err := flagparser.Merge(cmd, spec, c); err != nil {
				return err
			}

			// do all of this after we set up the spec
			spec.Task.GetContainer().Image = args[0]
			if len(args) > 1 {
				spec.Task.GetContainer().Args = args[1:]
			}

			r, err := c.CreateService(common.Context(cmd), &api.CreateServiceRequest{Spec: spec})
			if err != nil {
				return err
			}
			fmt.Println(r.Service.ID)
			return nil
		},
	}
)

func init() {
	flagparser.AddServiceFlags(createCmd.Flags())
}
