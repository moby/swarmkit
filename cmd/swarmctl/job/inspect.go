package job

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/cmd/swarmctl/common"
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
			common.FprintfIfNotEmpty(w, "ID:\t%s\n", job.ID)
			common.FprintfIfNotEmpty(w, "Name:\t%s\n", job.Spec.Meta.Name)
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
			common.FprintfIfNotEmpty(w, "Orchestration:\t%s\n", orchestration)
			fmt.Fprintln(w, "Template:")
			fmt.Fprintln(w, " Container:")
			common.FprintfIfNotEmpty(w, "  Image:\t%s\n", job.Spec.Template.GetContainer().Image.Reference)
			common.FprintfIfNotEmpty(w, "  Command:\t%q\n", strings.Join(job.Spec.Template.GetContainer().Command, " "))
			common.FprintfIfNotEmpty(w, "  Args:\t[%s]\n", strings.Join(job.Spec.Template.GetContainer().Args, ", "))
			common.FprintfIfNotEmpty(w, "  Env:\t[%s]\n", strings.Join(job.Spec.Template.GetContainer().Env, ", "))
			if len(job.Spec.Template.GetContainer().Networks) > 0 {
				fmt.Fprintln(w, "  Networks:")
				for _, n := range job.Spec.Template.GetContainer().Networks {
					fmt.Fprintf(w, " %s\n", n.GetName())
				}
			}

			return nil
		},
	}
)
