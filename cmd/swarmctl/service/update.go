package service

import (
	"errors"
	"fmt"
	"reflect"

	"github.com/docker/swarmkit/api"
	"github.com/docker/swarmkit/cmd/swarmctl/common"
	"github.com/docker/swarmkit/cmd/swarmctl/service/flagparser"
	"github.com/spf13/cobra"
)

var (
	updateCmd = &cobra.Command{
		Use:   "update [OPTIONS] SERVICE [IMAGE] [ARGS...]",
		Short: "Update a service",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return errors.New("service ID missing")
			}

			c, err := common.Dial(cmd)
			if err != nil {
				return err
			}

			service, err := getService(common.Context(cmd), c, args[0])
			if err != nil {
				return err
			}

			spec := service.Spec.Copy()

			if err := flagparser.Merge(cmd, spec, c); err != nil {
				return err
			}

			// change the image if that positional arg is present
			if len(args) > 1 {
				spec.Task.GetContainer().Image = args[1]
			}

			// change the args too, if those positional args are present
			if len(args) > 2 {
				spec.Task.GetContainer().Args = args[2:]
			}

			if reflect.DeepEqual(spec, &service.Spec) {
				return errors.New("no changes detected")
			}

			r, err := c.UpdateService(common.Context(cmd), &api.UpdateServiceRequest{
				ServiceID:      service.ID,
				ServiceVersion: &service.Meta.Version,
				Spec:           spec,
			})
			if err != nil {
				return err
			}
			fmt.Println(r.Service.ID)
			return nil
		},
	}
)

func init() {
	flagparser.AddServiceFlags(updateCmd.Flags())
}
