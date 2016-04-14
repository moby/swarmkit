package service

import (
	"errors"
	"fmt"

	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/cmd/swarmctl/common"
	"github.com/spf13/cobra"
)

var (
	updateCmd = &cobra.Command{
		Use:   "update <service ID>",
		Short: "Update a service",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return errors.New("service ID missing")
			}

			flags := cmd.Flags()
			var spec *api.ServiceSpec

			if flags.Changed("file") {
				service, err := readServiceConfig(flags)
				if err != nil {
					return err
				}
				spec = service.ToProto()
			} else { // TODO(vieux): support or error on both file.
				spec = &api.ServiceSpec{}

				if flags.Changed("instances") {
					instances, err := flags.GetInt64("instances")
					if err != nil {
						return err
					}
					spec.Instances = instances
				}
				if len(args) > 1 {
					spec.Template.GetContainer().Command = args[1:]
				}
				if flags.Changed("args") {
					containerArgs, err := flags.GetStringSlice("args")
					if err != nil {
						return err
					}
					spec.Template.GetContainer().Args = containerArgs
				}
				if flags.Changed("env") {
					env, err := flags.GetStringSlice("env")
					if err != nil {
						return err
					}
					spec.Template.GetContainer().Env = env
				}
			}
			c, err := common.Dial(cmd)
			if err != nil {
				return err
			}

			service, err := getService(common.Context(cmd), c, args[0])
			if err != nil {
				return err
			}
			r, err := c.UpdateService(common.Context(cmd), &api.UpdateServiceRequest{ServiceID: service.ID, Spec: spec})
			if err != nil {
				return err
			}
			fmt.Println(r.Service.ID)
			return nil
		},
	}
)

func init() {
	updateCmd.Flags().StringSlice("args", nil, "Args")
	updateCmd.Flags().StringSlice("env", nil, "Env")
	updateCmd.Flags().StringP("file", "f", "", "Spec to use")
	// TODO(aluzzardi): This should be called `service-instances` so that every
	// orchestrator can have its own flag namespace.
	updateCmd.Flags().Int64("instances", 0, "Number of instances for the service")
	// TODO(vieux): This could probably be done in one step
	updateCmd.Flags().Lookup("instances").DefValue = ""
}
