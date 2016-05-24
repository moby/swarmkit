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
	"github.com/docker/swarm-v2/cmd/swarmctl/task"
	"github.com/dustin/go-humanize"
	"github.com/spf13/cobra"
)

func printServiceSummary(service *api.Service) {
	w := tabwriter.NewWriter(os.Stdout, 8, 8, 8, ' ', 0)
	defer w.Flush()

	common.FprintfIfNotEmpty(w, "ID\t: %s\n", service.ID)
	common.FprintfIfNotEmpty(w, "Name\t: %s\n", service.Spec.Annotations.Name)
	common.FprintfIfNotEmpty(w, "Instances\t: %s\n", getServiceInstancesTxt(service))
	fmt.Fprintln(w, "Template\t")
	fmt.Fprintln(w, " Container\t")
	ctr := service.Spec.GetContainer()
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
		fmt.Fprintf(w, "  Networks:\t")
		for _, n := range ctr.Networks {
			fmt.Fprintf(w, " %s", n.GetNetworkID())
		}
	}

	if ctr.ExposedPorts != nil && len(ctr.ExposedPorts) > 0 {
		fmt.Fprintln(w, "\nPorts:")
		for _, port := range ctr.ExposedPorts {
			fmt.Fprintf(w, "    - Name\t= %s\n", port.Name)
			fmt.Fprintf(w, "      Protocol\t= %s\n", port.Protocol)
			fmt.Fprintf(w, "      Port\t= %d\n", port.Port)
			fmt.Fprintf(w, "      NodePort\t= %d\n", port.HostPort)
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
			} else if v.Type == api.MountTypeVolume {
				fmt.Fprintf(w, "    - target = %s\n", v.Target)
				fmt.Fprintf(w, "      type = volume\n")
				fmt.Fprintf(w, "      name = %s\n", v.VolumeName)
				fmt.Fprintf(w, "      mask = %s\n", v.Mask.String())
			}
		}
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

			instance, err := flags.GetUint64("instance")
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
				if instance != 0 && t.Instance != instance {
					continue
				}
				if t.ServiceID != service.ID {
					continue
				}
				tasks = append(tasks, t)
			}

			printServiceSummary(service)
			if len(tasks) > 0 {
				fmt.Printf("\n")
				task.Print(tasks, all || instance != 0, res)
			}

			return nil
		},
	}
)

func init() {
	inspectCmd.Flags().BoolP("all", "a", false, "Show all tasks (default shows just running)")
	inspectCmd.Flags().Uint64P("instance", "i", 0, "Show tasks with a specific instance number")
}
