package service

import (
	"errors"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/moby/swarmkit/swarmd/cmd/swarmctl/common"
	"github.com/moby/swarmkit/v2/api"
	"github.com/spf13/cobra"
)

var (
	listCmd = &cobra.Command{
		Use:   "ls",
		Short: "List services",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 0 {
				return errors.New("ls command takes no arguments")
			}

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

			nr, err := c.ListNodes(common.Context(cmd), &api.ListNodesRequest{})
			if err != nil {
				return err
			}
			liveNodes := make(map[string]struct{})
			for _, n := range nr.Nodes {
				if n.Status.GetState() != api.NodeStatus_DOWN {
					liveNodes[n.Id] = struct{}{}
				}
			}

			var output func(j *api.Service)

			if !quiet {
				tr, err := c.ListTasks(common.Context(cmd), &api.ListTasksRequest{})
				if err != nil {
					return err
				}

				running := map[string]int{}
				for _, task := range tr.Tasks {
					if _, nodeLive := liveNodes[task.NodeId]; nodeLive &&
						task.Status.GetState() == api.TaskState_RUNNING {
						running[task.ServiceId]++
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
						reference = spec.Task.GetContainer().GetImage()
					}

					fmt.Fprintf(w, "%s\t%s\t%s\t%s\n",
						s.Id,
						spec.GetAnnotations().GetName(),
						reference,
						getServiceReplicasTxt(s, running[s.Id]),
					)
				}

			} else {
				output = func(j *api.Service) { fmt.Println(j.Id) }
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
