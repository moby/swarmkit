package service

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/docker/swarmkit/api"
	"github.com/docker/swarmkit/cmd/swarmctl/common"
	"github.com/spf13/cobra"
)

var (
	listCmd = &cobra.Command{
		Use:   "ls",
		Short: "List services",
		RunE: func(cmd *cobra.Command, args []string) error {
			flags := cmd.Flags()

			quiet, err := flags.GetBool("quiet")
			if err != nil {
				return err
			}

			c, err := common.Dial(cmd)
			if err != nil {
				return err
			}
			r, err := c.ListServices(common.Context(cmd), &api.ListServicesRequest{})
			if err != nil {
				return err
			}

			var output func(j *api.Service)

			if !quiet {
				tr, err := c.ListTasks(common.Context(cmd), &api.ListTasksRequest{})
				if err != nil {
					return err
				}

				running := map[string]int{}
				for _, task := range tr.Tasks {
					if task.Status.State == api.TaskStateRunning {
						running[task.ServiceID]++
					}
				}

				w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
				defer func() {
					// Ignore flushing errors - there's nothing we can do.
					_ = w.Flush()
				}()
				common.PrintHeader(w, "ID", "Name", "Image", "Replicas")
				output = func(s *api.Service) {
					spec := s.Spec
					var reference string

					if spec.Task.GetContainer() != nil {
						reference = spec.Task.GetContainer().Image
					}

					fmt.Fprintf(w, "%s\t%s\t%s\t%s\n",
						s.ID,
						spec.Annotations.Name,
						reference,
						getServiceReplicasTxt(s, running[s.ID]),
					)
				}

			} else {
				output = func(j *api.Service) { fmt.Println(j.ID) }
			}

			for _, j := range r.Services {
				output(j)
			}
			return nil
		},
	}
)

func init() {
	listCmd.Flags().BoolP("quiet", "q", false, "Only display IDs")
}
