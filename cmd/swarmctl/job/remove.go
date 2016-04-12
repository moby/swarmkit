package job

import (
	"errors"
	"fmt"

	"github.com/docker/swarm-v2/cmd/swarmctl/common"
	"github.com/docker/swarm-v2/pb/docker/cluster/api"
	"github.com/spf13/cobra"
)

var (
	removeCmd = &cobra.Command{
		Use:     "remove <job ID>",
		Short:   "Remove a job",
		Aliases: []string{"rm"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return errors.New("job ID missing")
			}
			c, err := common.Dial(cmd)
			if err != nil {
				return err
			}

			job, err := getJob(common.Context(cmd), c, args[0])
			if err != nil {
				return err
			}
			_, err = c.RemoveJob(common.Context(cmd), &api.RemoveJobRequest{JobID: job.ID})
			if err != nil {
				return err
			}
			fmt.Println(args[0])
			return nil
		},
	}
)
