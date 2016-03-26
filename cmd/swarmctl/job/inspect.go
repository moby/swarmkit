package job

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

var (
	inspectCmd = &cobra.Command{
		Use:   "inspect <job ID>",
		Short: "Inspect a job",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return errors.New("job ID missing")
			}
			c, err := common.Dial(cmd)
			if err != nil {
				return err
			}

			job, err := getJob(common.Context(cmd), c, args[0])
			if err != nil {
				return err
			}
			w := tabwriter.NewWriter(os.Stdout, 8, 8, 8, ' ', 0)
			defer func() {
				// Ignore flushing errors - there's nothing we can do.
				_ = w.Flush()
			}()
			common.FprintfIfNotEmpty(w, "ID\t: %s\n", job.ID)
			common.FprintfIfNotEmpty(w, "Name\t: %s\n", job.Spec.Meta.Name)
			orchestration := ""
			switch o := job.Spec.Orchestration.(type) {
			case *api.JobSpec_Batch:
				orchestration = "BATCH"
			case *api.JobSpec_Cron:
				orchestration = "CRON"
			case *api.JobSpec_Global:
				orchestration = "GLOBAL"
			case *api.JobSpec_Service:
				orchestration = fmt.Sprintf("SERVICE (%d instances)", o.Service.Instances)
			}
			common.FprintfIfNotEmpty(w, "Orchestration\t: %s\n", orchestration)
			fmt.Fprintln(w, "Template:\t")
			fmt.Fprintln(w, " Container:\t")
			ctr := job.Spec.Template.GetContainer()
			common.FprintfIfNotEmpty(w, "  Image\t: %s\n", ctr.Image.Reference)
			common.FprintfIfNotEmpty(w, "  Command\t: %q\n", strings.Join(ctr.Command, " "))
			common.FprintfIfNotEmpty(w, "  Args\t: [%s]\n", strings.Join(ctr.Args, ", "))
			common.FprintfIfNotEmpty(w, "  Env\t: [%s]\n", strings.Join(ctr.Env, ", "))
			if ctr.Resources != nil {
				res := ctr.Resources
				fmt.Fprintln(w, "  Resources:\t")
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

			return nil
		},
	}
)
