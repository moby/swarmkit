package node

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/cmd/swarmctl/common"
	"github.com/spf13/cobra"
)

var (
	lsCmd = &cobra.Command{
		Use:   "ls",
		Short: "List nodes",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := common.Dial(cmd)
			if err != nil {
				return err
			}
			r, err := c.ListNodes(common.Context(cmd), &api.ListNodesRequest{})
			if err != nil {
				return err
			}

			w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
			defer func() {
				// Ignore flushing errors - there's nothing we can do.
				_ = w.Flush()
			}()
			fmt.Fprintln(w, "ID\tName\tStatus\tAvailability")
			for _, n := range r.Nodes {
				spec := n.Spec
				if spec == nil {
					spec = &api.NodeSpec{}
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
			return nil
		},
	}
)
