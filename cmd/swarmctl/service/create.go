package service

import (
	"errors"
	"fmt"

	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/cmd/swarmctl/common"
	"github.com/docker/swarm-v2/cmd/swarmctl/network"
	"github.com/spf13/cobra"
)

var (
	createCmd = &cobra.Command{
		Use:   "create",
		Short: "Create a service",
		RunE: func(cmd *cobra.Command, args []string) error {
			flags := cmd.Flags()
			var spec *api.ServiceSpec

			c, err := common.Dial(cmd)
			if err != nil {
				return err
			}

			if flags.Changed("file") {
				service, err := readServiceConfig(flags)
				if err != nil {
					return err
				}
				spec = service.ToProto()
			} else { // TODO(vieux): support or error on both file.
				if !flags.Changed("name") || !flags.Changed("image") {
					return errors.New("--name and --image are mandatory")
				}
				name, err := flags.GetString("name")
				if err != nil {
					return err
				}
				image, err := flags.GetString("image")
				if err != nil {
					return err
				}
				instances, err := flags.GetInt64("instances")
				if err != nil {
					return err
				}

				containerArgs, err := flags.GetStringSlice("args")
				if err != nil {
					return err
				}

				env, err := flags.GetStringSlice("env")
				if err != nil {
					return err
				}

				spec = &api.ServiceSpec{
					Annotations: api.Annotations{
						Name: name,
					},
					Template: &api.TaskSpec{
						Runtime: &api.TaskSpec_Container{
							Container: &api.Container{
								Image: &api.Image{
									Reference: image,
								},
								Command: containerArgs,
								Args:    args,
								Env:     env,
							},
						},
					},
					Instances: instances,
				}

				if flags.Changed("network") {
					input, err := flags.GetString("network")
					if err != nil {
						return err
					}

					n, err := network.GetNetwork(common.Context(cmd), c, input)
					if err != nil {
						return err
					}

					spec.Template.GetContainer().Networks = []*api.Container_NetworkAttachment{
						{
							Reference: &api.Container_NetworkAttachment_NetworkID{
								NetworkID: n.ID,
							},
						},
					}
				}
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
	createCmd.Flags().String("name", "", "Service name")
	createCmd.Flags().String("image", "", "Image")
	createCmd.Flags().StringSlice("args", nil, "Args")
	createCmd.Flags().StringSlice("env", nil, "Env")
	createCmd.Flags().StringP("file", "f", "", "Spec to use")
	createCmd.Flags().String("network", "", "Network name")
	// TODO(aluzzardi): This should be called `service-instances` so that every
	// orchestrator can have its own flag namespace.
	createCmd.Flags().Int64("instances", 1, "Number of instances for the service Service")
}
