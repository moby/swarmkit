package root

import (
	"fmt"

	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/cmd/swarmctl/common"
	"github.com/spf13/cobra"
)

var (
	deployCmd = &cobra.Command{
		Use:   "deploy",
		Short: "Deploy an app",
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
					return err
				}
				fmt.Printf("%s: %s\n", jobSpec.Meta.Name, r.Job.ID)
			}
			return nil
		},
	}
)

func init() {
	deployCmd.Flags().StringP("file", "f", "docker.yml", "Spec file to deploy")
}
