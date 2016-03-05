package job

import (
	"errors"
	"fmt"

	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/cmd/swarmctl/common"
	"github.com/spf13/cobra"
)

var (
	updateCmd = &cobra.Command{
		Use:   "update <job ID>",
		Short: "Update a job",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return errors.New("job ID missing")
			}

			flags := cmd.Flags()
			spec := &api.JobSpec{}

			if flags.Changed("instances") {
				instances, err := flags.GetInt64("instances")
				if err != nil {
					return err
				}
				spec.Orchestration = &api.JobSpec_Service{
					Service: &api.JobSpec_ServiceJob{
						Instances: instances,
					},
				}
			}

			c, err := common.Dial(cmd)
			if err != nil {
				return err
			}

			r, err := c.UpdateJob(common.Context(cmd), &api.UpdateJobRequest{JobID: args[0], Spec: spec})
			if err != nil {
				return err
			}
			fmt.Println(r.Job.ID)
			return nil
		},
	}
)

func init() {
	// TODO(aluzzardi): This should be called `service-instances` so that every
	// orchestrator can have its own flag namespace.
	updateCmd.Flags().Int64("instances", 0, "Number of instances for the service Job")
	// TODO(vieux): This could probably be done in one step
	updateCmd.Flags().Lookup("instances").DefValue = ""
}
