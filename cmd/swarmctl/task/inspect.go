package task

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/cmd/swarmctl/common"
	"github.com/docker/swarm-v2/protobuf/ptypes"
	"github.com/spf13/cobra"
)

func printTaskStatus(w io.Writer, t *api.Task) {
	fmt.Fprintln(w, "Status\t")
	fmt.Fprintf(w, "  Desired State\t: %s\n", t.DesiredState.String())
	fmt.Fprintf(w, "  Last State\t: %s\n", t.Status.State.String())
	if t.Status.Timestamp != nil {
		fmt.Fprintf(w, "  Timestamp\t: %s\n", ptypes.TimestampString(t.Status.Timestamp))
	}
	if t.Status.Message != "" {
		fmt.Fprintf(w, "  Message\t: %s\n", t.Status.Message)
	}
	if t.Status.Err != "" {
		fmt.Fprintf(w, "  Error\t: %s\n", t.Status.Err)
	}
}

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

			res := common.NewResolver(cmd, c)

			w := tabwriter.NewWriter(os.Stdout, 8, 8, 8, ' ', 0)
			defer func() {
				// Ignore flushing errors - there's nothing we can do.
				_ = w.Flush()
			}()
			fmt.Fprintf(w, "ID\t: %s\n", r.Task.ID)
			fmt.Fprintf(w, "Instance\t: %d\n", r.Task.Instance)
			fmt.Fprintf(w, "Service\t: %s\n", res.Resolve(api.Service{}, r.Task.ServiceID))
			printTaskStatus(w, r.Task)
			fmt.Fprintf(w, "Node\t: %s\n", res.Resolve(api.Node{}, r.Task.NodeID))

			fmt.Fprintln(w, "Spec\t")
			ctr := r.Task.Spec.GetContainer()
			common.FprintfIfNotEmpty(w, "  Image\t: %s\n", ctr.Image.Reference)
			common.FprintfIfNotEmpty(w, "  Command\t: %q\n", strings.Join(ctr.Command, " "))
			common.FprintfIfNotEmpty(w, "  Args\t: [%s]\n", strings.Join(ctr.Args, ", "))
			common.FprintfIfNotEmpty(w, "  Env\t: [%s]\n", strings.Join(ctr.Env, ", "))

			return nil
		},
	}
)
