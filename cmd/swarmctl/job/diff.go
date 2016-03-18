package job

import (
	"errors"
	"fmt"

	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/cmd/swarmctl/common"
	"github.com/docker/swarm-v2/spec"
	"github.com/spf13/cobra"
)

var (
	diffCmd = &cobra.Command{
		Use:   "diff <job ID>",
		Short: "Diff a job",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return errors.New("job ID missing")
			}

			flags := cmd.Flags()

			if !flags.Changed("file") {
				return errors.New("--file is mandatory")
			}

			context, err := flags.GetInt("context")
			if err != nil {
				return err
			}

			c, err := common.Dial(cmd)
			if err != nil {
				return err
			}

			r, err := c.GetJob(common.Context(cmd), &api.GetJobRequest{JobID: args[0]})
			if err != nil {
				return err
			}

			localService, err := readServiceConfig(flags)
			if err != nil {
				return err
			}

			remoteService := &spec.ServiceConfig{}
			remoteService.FromJobSpec(r.Job.Spec)
			diff, err := localService.Diff(context, "remote", "local", remoteService)
			if err != nil {
				return err
			}
			fmt.Print(diff)
			return nil
		},
	}
)

func init() {
	diffCmd.Flags().StringP("file", "f", "", "Spec to use")
	diffCmd.Flags().IntP("context", "c", 3, "lines of copied context (default 3)")
}
