package node

import (
	"errors"
	"fmt"
	"github.com/docker/swarmkit/api"
	"github.com/docker/swarmkit/cmd/swarmctl/common"
	"github.com/dustin/go-humanize"
	"github.com/spf13/cobra"
	"os"
	"strconv"
	"text/tabwriter"
)

var (
	listCmd = &cobra.Command{
		Use:   "ls",
		Short: "List nodes",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 0 {
				return errors.New("ls command takes no arguments")
			}

			flags := cmd.Flags()

			quiet, err := flags.GetBool("quiet")
			if err != nil {
				return err
			}
			availableResourcesFlag, err := flags.GetBool("available-resources")
			if err != nil {
				return err
			}

			c, err := common.Dial(cmd)
			if err != nil {
				return err
			}
			r, err := c.ListNodes(common.Context(cmd), &api.ListNodesRequest{
				AvailableResources: availableResourcesFlag,
			})
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
				if !availableResourcesFlag {
					common.PrintHeader(w, "ID", "Name", "Membership", "Status", "Availability", "Manager Status", "CPUs", "Memory")
				} else {
					common.PrintHeader(w, "ID", "Name", "Membership", "Status", "Availability", "Manager Status", "CPUs", "Memory", "Available CPUs", "Available Memory")
				}
				output = func(n *api.Node) {
					spec := &n.Spec
					name := spec.Annotations.Name
					availability := spec.Availability.String()
					membership := spec.Membership.String()

					if name == "" && n.Description != nil {
						name = n.Description.Hostname
					}
					reachability := ""
					if n.ManagerStatus != nil {
						reachability = n.ManagerStatus.Reachability.String()
						if n.ManagerStatus.Leader {
							reachability = reachability + " *"
						}
					}
					if reachability == "" && spec.DesiredRole == api.NodeRoleManager {
						reachability = "UNKNOWN"
					}
					cpus := ""
					memory := ""
					if n.Description != nil && n.Description.Resources != nil {
						cpus = strconv.FormatFloat(float64(n.Description.Resources.NanoCPUs)/1e9, 'f', -1, 64)
						memory = humanize.IBytes(uint64(n.Description.Resources.MemoryBytes))
					}
					if !availableResourcesFlag {
						fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
							n.ID,
							name,
							membership,
							n.Status.State.String(),
							availability,
							reachability,
							cpus,
							memory,
						)
					} else {
						availableCPUs := ""
						availableMemory := ""
						if n.AvailableResources != nil {
							availableCPUs = strconv.FormatFloat(float64(n.AvailableResources.NanoCPUs)/1e9, 'f', -1, 64)
							availableMemory = humanize.IBytes(uint64(n.AvailableResources.MemoryBytes))
						}
						fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
							n.ID,
							name,
							membership,
							n.Status.State.String(),
							availability,
							reachability,
							cpus,
							memory,
							availableCPUs,
							availableMemory,
						)
					}
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
	listCmd.Flags().BoolP("available-resources", "r", false, "Display available resources")
}
