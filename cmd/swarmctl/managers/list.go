package managers

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
		Short: "List managers",
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
			r, err := c.ListManagers(common.Context(cmd), &api.ListManagersRequest{})
			if err != nil {
				return err
			}

			var output func(n *api.Member)

			if !quiet {
				w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
				defer func() {
					// Ignore flushing errors - there's nothing we can do.
					_ = w.Flush()
				}()

				// TODO(abronan): include member name and raft cluster it belongs to
				common.PrintHeader(w, "ID", "Address", "Status")
				output = func(n *api.Member) {
					fmt.Fprintf(w, "%s\t%s\t%s\n",
						n.ID,
						n.Addr,
						n.Status.State,
					)
				}
			} else {
				output = func(n *api.Member) { fmt.Println(n.ID) }
			}

			for _, n := range r.Managers {
				output(n)
			}
			return nil
		},
	}
)

func init() {
	listCmd.Flags().BoolP("quiet", "q", false, "Only display IDs")
}
