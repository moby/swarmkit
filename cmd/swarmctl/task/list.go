package task

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/cmd/swarmctl/common"
	"github.com/spf13/cobra"
)

var (
	listCmd = &cobra.Command{
		Use:   "ls",
		Short: "List tasks",
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
			r, err := c.ListTasks(common.Context(cmd), &api.ListTasksRequest{})
			if err != nil {
				return err
			}
			res := common.NewResolver(cmd, c)

			var output func(t *api.Task)

			if !quiet {
				w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
				defer func() {
					// Ignore flushing errors - there's nothing we can do.
					_ = w.Flush()
				}()
				common.PrintHeader(w, "ID", "Service", "Status", "Node")
				output = func(t *api.Task) {
					fmt.Fprintf(w, "%s\t%s\t%s\t%s\n",
						t.ID,
						res.Resolve(api.Service{}, t.ServiceID),
						t.Status.State.String(),
						res.Resolve(api.Node{}, t.NodeID),
					)
				}
			} else {
				output = func(t *api.Task) { fmt.Println(t.ID) }
			}

			for _, t := range r.Tasks {
				output(t)
			}
			return nil
		},
	}
)

func init() {
	listCmd.Flags().BoolP("quiet", "q", false, "Only display IDs")
}
