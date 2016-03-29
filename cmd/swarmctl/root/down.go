package root

import (
	"fmt"

	"google.golang.org/grpc"

	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/cmd/swarmctl/common"
	"github.com/spf13/cobra"
)

var (
	downCmd = &cobra.Command{
		Use:   "down [service...]",
		Short: "Bring one or more services down",
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
				_, err = c.RemoveJob(common.Context(cmd), &api.RemoveJobRequest{JobID: job.ID})
				if err != nil {
					fmt.Printf("%s: %v\n", job.Spec.Meta.Name, grpc.ErrorDesc(err))
					continue
				}
				fmt.Printf("%s: %s - REMOVED\n", job.Spec.Meta.Name, job.ID)
			}
			return nil
		},
	}
)

func init() {
	downCmd.Flags().StringP("file", "f", "docker.yml", "Spec file")
}
