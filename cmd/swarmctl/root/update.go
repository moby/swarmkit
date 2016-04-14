package root

import (
	"fmt"

	"google.golang.org/grpc"

	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/cmd/swarmctl/common"
	"github.com/spf13/cobra"
)

var (
	updateCmd = &cobra.Command{
		Use:   "update [service...]",
		Short: "Update one or more services",
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := readSpec(cmd.Flags())
			if err != nil {
				return err
			}

			c, err := common.Dial(cmd)
			if err != nil {
				return err
			}

			for _, jobSpec := range s.JobSpecs() {
				job := getJobByName(common.Context(cmd), c, jobSpec.Meta.Name)
				if job == nil {
					fmt.Printf("%s: Not found\n", jobSpec.Meta.Name)
					continue
				}
				r, err := c.UpdateJob(common.Context(cmd), &api.UpdateJobRequest{JobID: job.ID, Spec: jobSpec})
				if err != nil {
					fmt.Printf("%s: %v\n", jobSpec.Meta.Name, grpc.ErrorDesc(err))
					continue
				}
				fmt.Printf("%s: %s - UPDATED\n", jobSpec.Meta.Name, r.Job.ID)
			}

			return nil
		},
	}
)

func init() {
	updateCmd.Flags().StringP("file", "f", "docker.yml", "Spec file")
}
