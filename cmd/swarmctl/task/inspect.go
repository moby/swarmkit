package task

import (
	"errors"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/cmd/swarmctl/common"
	"github.com/spf13/cobra"
)

var (
	inspectCmd = &cobra.Command{
		Use:   "inspect <task ID>",
		Short: "Inspect a task",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return errors.New("task ID missing")
			}
			c, err := common.Dial(cmd)
			if err != nil {
				return err
			}

			r, err := c.GetTask(common.Context(cmd), &api.GetTaskRequest{TaskID: args[0]})
			if err != nil {
				return err
			}
			w := tabwriter.NewWriter(os.Stdout, 8, 8, 8, ' ', 0)
			defer func() {
				// Ignore flushing errors - there's nothing we can do.
				_ = w.Flush()
			}()
			fmt.Fprintf(w, "ID:\t%s\n", r.Task.ID)
			fmt.Fprintf(w, "JobID:\t%s\n", r.Task.JobID)
			if r.Task.Status.Err != "" {
				fmt.Fprintf(w, "Status:\t%s (%s)\n", r.Task.Status.State.String(), r.Task.Status.Err)
			} else {
				fmt.Fprintf(w, "Status:\t%s\n", r.Task.Status.State.String())
			}
			fmt.Fprintf(w, "NodeID:\t%s\n", r.Task.NodeID)
			return nil
		},
	}
)
