package managers

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
		Use:   "inspect <manager ID>",
		Short: "Inspect a manager",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return errors.New("manager ID missing")
			}
			c, err := common.Dial(cmd)
			if err != nil {
				return err
			}

			r, err := c.GetManager(common.Context(cmd), &api.GetManagerRequest{ManagerID: args[0]})
			if err != nil {
				return err
			}

			w := tabwriter.NewWriter(os.Stdout, 8, 8, 8, ' ', 0)

			defer func() {
				// Ignore flushing errors - there's nothing we can do.
				_ = w.Flush()
			}()
			role := "Follower"
			if r.Manager.Status.Leader {
				role = "Leader"
			}
			fmt.Fprintf(w, "ID\t: %s\n", r.Manager.ID)
			fmt.Fprintf(w, "Address\t: %s\n", r.Manager.Addr)
			fmt.Fprintf(w, "Status\t: %s\n", r.Manager.Status.State)
			fmt.Fprintf(w, "Role\t: %s\n", role)

			fmt.Fprintln(w, "Configuration:\t")
			common.FprintfIfNotEmpty(w, "  Election Tick\t: %d\n", r.Manager.Description.ElectionTick)
			common.FprintfIfNotEmpty(w, "  Heartbeat Tick\t: %d\n", r.Manager.Description.HeartbeatTick)
			common.FprintfIfNotEmpty(w, "  Max Size Per Message\t: %d\n", r.Manager.Description.MaxSizePerMsg)

			fmt.Fprintln(w, "Log Messages:\t")
			fmt.Fprintf(w, "  Since Last Snapshot\t: %d\n", r.Manager.Status.AppliedIndex-r.Manager.Status.SnapshotIndex)
			fmt.Fprintf(w, "  Until New Snapshot\t: %d\n", r.Manager.Status.SnapshotIndex+r.Manager.Description.SnapshotInterval-r.Manager.Status.AppliedIndex)

			return nil
		},
	}
)
