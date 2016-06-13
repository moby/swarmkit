package node

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/docker/swarmkit/api"
	"github.com/docker/swarmkit/cmd/swarmctl/common"
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

			var output func(n *api.Node)

			if !quiet {
				w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
				defer func() {
					// Ignore flushing errors - there's nothing we can do.
					_ = w.Flush()
				}()
				common.PrintHeader(w, "ID", "Name", "Membership", "Status", "Manager status")
				output = func(n *api.Node) {
					spec := &n.Spec
					name := spec.Annotations.Name
					membership := spec.Membership.String()

					if name == "" && n.Description != nil {
						name = n.Description.Hostname
					}

					nodeStatus := n.Status.State.String()
					// node Active is default. Only show other state, like drain, pause.
					if spec.Availability != api.NodeAvailabilityActive {
						nodeStatus = nodeStatus + "(" + spec.Availability.String() + ")"
					}

					reachability := ""
					if n.Manager != nil {
						reachability = n.Manager.Raft.Status.Reachability.String()
						if n.Manager.Raft.Status.Leader {
							reachability = reachability + " *"
						}
					}
					if reachability == "" && spec.Role == api.NodeRoleManager {
						reachability = "UNKNOWN"
					}

					fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
						n.ID,
						name,
						membership,
						nodeStatus,
						reachability,
					)
				}
			} else {
				output = func(n *api.Node) { fmt.Println(n.ID) }
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
