package job

import (
	"fmt"
	"os"
	"text/tabwriter"

	log "github.com/Sirupsen/logrus"
	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/cmd/swarmctl/common"
	"github.com/spf13/cobra"
)

var (
	lsCmd = &cobra.Command{
		Use:   "ls",
		Short: "List jobs",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := common.Dial(cmd)
			if err != nil {
				return err
			}
			r, err := c.ListJobs(common.Context(cmd), &api.ListJobsRequest{})
			if err != nil {
				return err
			}

			w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
			defer func() {
				// Ignore flushing errors - there's nothing we can do.
				_ = w.Flush()
			}()
			fmt.Fprintln(w, "ID\tName\tImage\tInstances")
			for _, j := range r.Jobs {
				spec := j.Spec
				service := spec.GetService()
				image := spec.GetImage()

				// TODO(aluzzardi): Right now we only implement the happy path
				// and don't have any proper error handling whatsover.
				// Instead of aborting, we should display what we can of the job.
				if service == nil || image == nil {
					log.Fatalf("Malformed job: %v", j)
				}

				fmt.Fprintf(w, "%s\t%s\t%s\t%d\n",
					j.ID,
					spec.Meta.Name,
					image.Reference,
					service.Instances,
				)
			}
			return nil
		},
	}
)
