package task

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/docker/swarm-v2/cmd/swarmctl/common"
	"github.com/docker/swarm-v2/pb/docker/cluster/api"
	objectspb "github.com/docker/swarm-v2/pb/docker/cluster/objects"
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

			var output func(t *objectspb.Task)

			if !quiet {
				w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
				defer func() {
					// Ignore flushing errors - there's nothing we can do.
					_ = w.Flush()
				}()
				fmt.Fprintln(w, "ID\tJob\tStatus\tNode")
				output = func(t *objectspb.Task) {
					fmt.Fprintf(w, "%s\t%s\t%s\t%s\n",
						t.ID,
						res.Resolve(objectspb.Job{}, t.JobID),
						t.Status.State.String(),
						res.Resolve(objectspb.Node{}, t.NodeID),
					)
				}
			} else {
				output = func(t *objectspb.Task) { fmt.Println(t.ID) }
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
