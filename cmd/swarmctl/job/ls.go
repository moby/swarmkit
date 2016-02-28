package job

import (
	"fmt"

	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/cmd/swarmctl/common"
	"github.com/spf13/cobra"
)

var (
	lsCmd = &cobra.Command{
		Use:   "ls",
		Short: "List jobs",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := common.Dial(cmd)
			if err != nil {
				return err
			}
			r, err := c.ListJobs(common.Context(cmd), &api.ListJobsRequest{})
			if err != nil {
				return err
			}
			for _, j := range r.Jobs {
				fmt.Println(j.ID)
			}
			return nil
		},
	}
)
