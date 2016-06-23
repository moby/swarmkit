package service

import (
	"errors"
	"fmt"

	"github.com/docker/swarmkit/api"
	"github.com/docker/swarmkit/cmd/swarmctl/common"
	"github.com/spf13/cobra"
)

var (
	logsCmd = &cobra.Command{
		Use:   "logs <service ID>",
		Short: "",
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

			r, err := c.ListTasks(common.Context(cmd),
				&api.ListTasksRequest{
					Filters: &api.ListTasksRequest_Filters{
						ServiceIDs: []string{service.ID},
					},
				})
			if err != nil {
				return err
			}
			for _, task := range r.Tasks {
				// TODO (gianarb) Magic stuff to grab log
				fmt.Println(task.Status.GetContainer().ContainerID)
			}
			return nil
		},
	}
)
