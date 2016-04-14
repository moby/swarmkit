package root

import (
	"fmt"

	"google.golang.org/grpc"

	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/cmd/swarmctl/common"
	"github.com/spf13/cobra"
)

var (
	upCmd = &cobra.Command{
		Use:   "up [service...]",
		Short: "Bring one or more services up",
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
				r, err := c.CreateJob(common.Context(cmd), &api.CreateJobRequest{Spec: jobSpec})
				if err != nil {
					fmt.Printf("%s: %v\n", jobSpec.Meta.Name, grpc.ErrorDesc(err))
					continue
				}
				fmt.Printf("%s: %s - CREATED\n", jobSpec.Meta.Name, r.Job.ID)
			}

			return nil
		},
	}
)

func init() {
	upCmd.Flags().StringP("file", "f", "docker.yml", "Spec file")
}
