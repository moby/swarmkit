package job

import (
	"errors"
	"fmt"

	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/cmd/swarmctl/common"
	"github.com/spf13/cobra"
)

var (
	deleteCmd = &cobra.Command{
		Use:   "delete <jobID>",
		Short: "Delete a job",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return errors.New("jobID missing")
			}
			c, err := common.Dial(cmd)
			if err != nil {
				return err
			}

			_, err = c.DeleteJob(common.Context(cmd), &api.DeleteJobRequest{JobID: args[0]})
			if err != nil {
				return err
			}
			fmt.Println(args[0])
			return nil
		},
	}
)
