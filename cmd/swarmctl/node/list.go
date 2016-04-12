package node

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/docker/swarm-v2/cmd/swarmctl/common"
	"github.com/docker/swarm-v2/pb/docker/cluster/api"
	objectspb "github.com/docker/swarm-v2/pb/docker/cluster/objects"
	specspb "github.com/docker/swarm-v2/pb/docker/cluster/specs"
	"github.com/spf13/cobra"
)

var (
	listCmd = &cobra.Command{
		Use:   "ls",
		Short: "List nodes",
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
			r, err := c.ListNodes(common.Context(cmd), &api.ListNodesRequest{})
			if err != nil {
				return err
			}

			var output func(n *objectspb.Node)

			if !quiet {
				w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
				defer func() {
					// Ignore flushing errors - there's nothing we can do.
					_ = w.Flush()
				}()
				fmt.Fprintln(w, "ID\tName\tStatus\tAvailability")
				output = func(n *objectspb.Node) {
					spec := n.Spec
					if spec == nil {
						spec = &specspb.NodeSpec{}
					}
					name := spec.Meta.Name
					if name == "" {
						name = n.Description.Hostname
					}
					fmt.Fprintf(w, "%s\t%s\t%s\t%s\n",
						n.ID,
						name,
						n.Status.State.String(),
						spec.Availability.String(),
					)
				}
			} else {
				output = func(n *objectspb.Node) { fmt.Println(n.ID) }
			}

			for _, n := range r.Nodes {
				output(n)
			}
			return nil
		},
	}
)

func init() {
	listCmd.Flags().BoolP("quiet", "q", false, "Only display IDs")
}
