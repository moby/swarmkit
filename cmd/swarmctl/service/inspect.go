package service

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/cmd/swarmctl/common"
	"github.com/dustin/go-humanize"
	"github.com/spf13/cobra"
)

func printServiceSummary(service *api.Service) {
	w := tabwriter.NewWriter(os.Stdout, 8, 8, 8, ' ', 0)
	defer func() {
		// Ignore flushing errors - there's nothing we can do.
		_ = w.Flush()
	}()
	common.FprintfIfNotEmpty(w, "ID\t: %s\n", service.ID)
	common.FprintfIfNotEmpty(w, "Name\t: %s\n", service.Spec.Annotations.Name)
	common.FprintfIfNotEmpty(w, "Instances\t: %d\n", service.Spec.Instances)
	common.FprintfIfNotEmpty(w, "Strategy\t: %s\n", service.Spec.Strategy)
	fmt.Fprintln(w, "Template\t")
	fmt.Fprintln(w, " Container\t")
	ctr := service.Spec.Template.GetContainer()
	common.FprintfIfNotEmpty(w, "  Image\t: %s\n", ctr.Image.Reference)
	common.FprintfIfNotEmpty(w, "  Command\t: %q\n", strings.Join(ctr.Command, " "))
	common.FprintfIfNotEmpty(w, "  Args\t: [%s]\n", strings.Join(ctr.Args, ", "))
	common.FprintfIfNotEmpty(w, "  Env\t: [%s]\n", strings.Join(ctr.Env, ", "))
	if ctr.Resources != nil {
		res := ctr.Resources
		fmt.Fprintln(w, "  Resources\t")
		printResources := func(w io.Writer, r *api.Resources) {
			if r.NanoCPUs != 0 {
				fmt.Fprintf(w, "      CPU\t: %g\n", float64(r.NanoCPUs)/1e9)
			}
			if r.MemoryBytes != 0 {
				fmt.Fprintf(w, "      Memory\t: %s\n", humanize.IBytes(uint64(r.MemoryBytes)))
			}
		}
		if res.Reservations != nil {
			fmt.Fprintln(w, "    Reservations:\t")
			printResources(w, res.Reservations)
		}
		if res.Limits != nil {
			fmt.Fprintln(w, "    Limits:\t")
			printResources(w, res.Limits)
		}
	}
	if len(ctr.Networks) > 0 {
		fmt.Fprintln(w, "  Networks:\t")
		for _, n := range ctr.Networks {
			fmt.Fprintf(w, " %s\n", n.GetName())
		}
	}
	if len(ctr.Mounts) > 0 {
		fmt.Fprintln(w, "  Mounts:")
		for _, v := range ctr.Mounts {
			if v.Type == api.MountTypeBind {
				fmt.Fprintf(w, "    - target = %s\n", v.Target)
				fmt.Fprintf(w, "      source = %s\n", v.Source)
				fmt.Fprintf(w, "      mask = %s\n", v.Mask.String())
				fmt.Fprintf(w, "      type = bind\n")
			} else if v.Type == api.MountTypeEphemeral {
				fmt.Fprintf(w, "    - target = %s\n", v.Target)
				fmt.Fprintf(w, "      type = ephemeral\n")
			}
		}
	}
}

func printTasks(tasks []*api.Task, all bool, res *common.Resolver) {
	w := tabwriter.NewWriter(os.Stdout, 4, 4, 4, ' ', 0)
	defer w.Flush()

	common.PrintHeader(w, "Task ID", "Image", "Status", "Node")
	for _, t := range tasks {
		if !all && t.Status.State > api.TaskStateRunning {
			continue
		}
		c := t.Spec.GetContainer()
		fmt.Fprintf(w, "%s\t%s\t%s %s\t%s\n",
			t.ID,
			c.Image.Reference,
			t.Status.State.String(),
			common.TimestampAgo(t.Status.Timestamp),
			res.Resolve(api.Node{}, t.NodeID),
		)
	}
}

var (
	inspectCmd = &cobra.Command{
		Use:   "inspect <service ID>",
		Short: "Inspect a service",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return errors.New("service ID missing")
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

			res := common.NewResolver(cmd, c)

			service, err := getService(common.Context(cmd), c, args[0])
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
				if t.ServiceID == service.ID {
					tasks = append(tasks, t)
				}
			}

			printServiceSummary(service)
			if len(tasks) > 0 {
				fmt.Printf("\n")
				printTasks(tasks, all, res)
			}

			return nil
		},
	}
)

func init() {
	inspectCmd.Flags().BoolP("all", "a", false, "Show all tasks (default shows just running)")
}
