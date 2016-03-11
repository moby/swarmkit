package job

import (
	"errors"
	"fmt"

	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/cmd/swarmctl/common"
	"github.com/spf13/cobra"
)

var (
	createCmd = &cobra.Command{
		Use:   "create",
		Short: "Create a job",
		RunE: func(cmd *cobra.Command, args []string) error {
			// TODO(aluzzardi): Check cobra best practices for flags handling.
			flags := cmd.Flags()
			if !flags.Changed("name") || !flags.Changed("image") {
				return errors.New("--name and --image are mandatory")
			}
			name, err := flags.GetString("name")
			if err != nil {
				return err
			}
			image, err := flags.GetString("image")
			if err != nil {
				return err
			}
			instances, err := flags.GetInt64("instances")
			if err != nil {
				return err
			}

			c, err := common.Dial(cmd)
			if err != nil {
				return err
			}

			spec := &api.JobSpec{
				Meta: api.Meta{
					Name: name,
				},
				Template: &api.TaskSpec{
					Runtime: &api.TaskSpec_Container{
						Container: &api.ContainerSpec{
							Image: &api.ImageSpec{
								Reference: image,
							},
						},
					},
				},
				Orchestration: &api.JobSpec_Service{
					Service: &api.JobSpec_ServiceJob{
						Instances: instances,
					},
				},
			}
			r, err := c.CreateJob(common.Context(cmd), &api.CreateJobRequest{Spec: spec})
			if err != nil {
				return err
			}
			fmt.Println(r.Job.ID)
			return nil
		},
	}
)

func init() {
	createCmd.Flags().String("name", "", "Job name")
	createCmd.Flags().String("image", "", "Image")
	// TODO(aluzzardi): This should be called `service-instances` so that every
	// orchestrator can have its own flag namespace.
	createCmd.Flags().Int64("instances", 1, "Number of instances for the service Job")
}
