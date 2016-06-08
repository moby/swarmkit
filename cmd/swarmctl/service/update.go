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
		Use:   "update <service ID>",
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
