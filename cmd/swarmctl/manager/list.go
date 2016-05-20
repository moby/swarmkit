package managers

import (
	"fmt"
	"os"
	"sort"
	"text/tabwriter"

	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/cmd/swarmctl/common"
	"github.com/spf13/cobra"
)

type managersByID []*api.Manager

func (m managersByID) Len() int {
	return len(m)
}

func (m managersByID) Swap(i, j int) {
	m[i], m[j] = m[j], m[i]
}

func (m managersByID) Less(i, j int) bool {
	return m[i].ID < m[j].ID
}

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

			var output func(n *api.Manager)

			if !quiet {
				w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
				defer func() {
					// Ignore flushing errors - there's nothing we can do.
					_ = w.Flush()
				}()

				// TODO(abronan): include member name and raft cluster it belongs to
				common.PrintHeader(w, "ID", "Address", "Status", "Leader")
				output = func(n *api.Manager) {
					leader := ""
					if n.Raft.Status.Leader {
						leader = "*"
					}
					fmt.Fprintf(w, "%s\t%s\t%s\t%s\n",
						n.ID,
						n.Raft.Addr,
						n.Raft.Status.State,
						leader,
					)
				}
			} else {
				output = func(n *api.Manager) { fmt.Println(n.ID) }
			}

			sortedManagers := managersByID(r.Managers)
			sort.Sort(sortedManagers)
			for _, n := range sortedManagers {
				output(n)
			}
			return nil
		},
	}
)

func init() {
	listCmd.Flags().BoolP("quiet", "q", false, "Only display IDs")
}
