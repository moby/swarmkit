package job

import (
	"errors"
	"fmt"

	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/cmd/swarmctl/common"
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

			id := common.LookupID(common.Context(cmd), c, api.Job{}, args[0])
			_, err = c.RemoveJob(common.Context(cmd), &api.RemoveJobRequest{JobID: id})
			if err != nil {
				return err
			}
			fmt.Println(id)
			return nil
		},
	}
)
