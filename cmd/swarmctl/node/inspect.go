package node

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"text/tabwriter"

	"github.com/docker/swarmkit/api"
	"github.com/docker/swarmkit/cmd/swarmctl/common"
	"github.com/docker/swarmkit/cmd/swarmctl/task"
	"github.com/dustin/go-humanize"
	"github.com/spf13/cobra"
)

func printNodeSummary(node *api.Node) {
	w := tabwriter.NewWriter(os.Stdout, 8, 8, 8, ' ', 0)
	defer func() {
		// Ignore flushing errors - there's nothing we can do.
		_ = w.Flush()
	}()
	spec := &node.Spec
	desc := node.Description
	if desc == nil {
		desc = &api.NodeDescription{}
	}
	common.FprintfIfNotEmpty(w, "ID\t: %s\n", node.ID)
	common.FprintfIfNotEmpty(w, "Name\t: %s\n", spec.Annotations.Name)
	if node.Description != nil {
		common.FprintfIfNotEmpty(w, "Hostname\t: %s\n", node.Description.Hostname)
	}
	fmt.Fprintln(w, "Status:\t")
	common.FprintfIfNotEmpty(w, "  State\t: %s\n", node.Status.State.String())
	common.FprintfIfNotEmpty(w, "  Message\t: %s\n", node.Status.Message)
	common.FprintfIfNotEmpty(w, "  Availability\t: %s\n", spec.Availability.String())

	if node.Manager != nil {
		fmt.Fprintln(w, "Manager:\t")
		common.FprintfIfNotEmpty(w, "  Address\t: %s\n", node.Manager.Raft.Addr)
		common.FprintfIfNotEmpty(w, "  Raft status\t: %s\n", node.Manager.Raft.Status.Reachability.String())
		leader := "no"
		if node.Manager.Raft.Status.Leader {
			leader = "yes"
		}
		common.FprintfIfNotEmpty(w, "  Leader\t: %s\n", leader)
	}

	if desc.Platform != nil {
		fmt.Fprintln(w, "Platform:\t")
		common.FprintfIfNotEmpty(w, "  Operating System\t: %s\n", desc.Platform.OS)
		common.FprintfIfNotEmpty(w, "  Architecture\t: %s\n", desc.Platform.Architecture)
	}

	if desc.Resources != nil && len(desc.Resources.ScalarResources) > 0 {
		fmt.Fprintln(w, "Resources:\t")
		for k, v := range desc.Resources.ScalarResources {
			if k == api.NanoCPUs.String() {
				fmt.Fprintf(w, "  CPUs\t: %g\n", v/1e9)
			} else if k == api.MemoryBytes.String() {
				fmt.Fprintf(w, "  Memory\t: %s\n", humanize.IBytes(uint64(v)))
			} else {
				fmt.Fprintf(w, "  %s\t: %g\n", k, v)
			}
		}
	}

	if desc.Engine != nil {
		fmt.Fprintln(w, "Plugins:\t")
		var pluginTypes []string
		pluginNamesByType := map[string][]string{}
		for _, p := range desc.Engine.Plugins {
			// append to pluginTypes only if not done previously
			if _, ok := pluginNamesByType[p.Type]; !ok {
				pluginTypes = append(pluginTypes, p.Type)
			}
			pluginNamesByType[p.Type] = append(pluginNamesByType[p.Type], p.Name)
		}

		sort.Strings(pluginTypes) // ensure stable output
		for _, pluginType := range pluginTypes {
			fmt.Fprintf(w, "  %s\t: %v\n", pluginType, pluginNamesByType[pluginType])
		}
	}

	if desc.Engine != nil {
		common.FprintfIfNotEmpty(w, "Engine Version\t: %s\n", desc.Engine.EngineVersion)

		if len(desc.Engine.Labels) != 0 {
			fmt.Fprintln(w, "Engine Labels:\t")
			for k, v := range desc.Engine.Labels {
				fmt.Fprintf(w, "  %s = %s", k, v)
			}
		}
	}
}

var (
	inspectCmd = &cobra.Command{
		Use:   "inspect <node ID>",
		Short: "Inspect a node",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return errors.New("node ID missing")
			}

			flags := cmd.Flags()

			all, err := flags.GetBool("all")
			if err != nil {
				return err
			}

			c, err := common.Dial(cmd)
			if err != nil {
				return err
			}

			node, err := getNode(common.Context(cmd), c, args[0])
			if err != nil {
				return err
			}

			// TODO(aluzzardi): This should be implemented as a ListOptions filter.
			r, err := c.ListTasks(common.Context(cmd), &api.ListTasksRequest{})
			if err != nil {
				return err
			}
			tasks := []*api.Task{}
			for _, t := range r.Tasks {
				if t.NodeID == node.ID {
					tasks = append(tasks, t)
				}
			}

			printNodeSummary(node)
			if len(tasks) > 0 {
				fmt.Printf("\n")
				task.Print(tasks, all, common.NewResolver(cmd, c))
			}

			return nil
		},
	}
)

func init() {
	inspectCmd.Flags().BoolP("all", "a", false, "Show all tasks (default shows just running)")
}
