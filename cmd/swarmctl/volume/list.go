package volume

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/cmd/swarmctl/common"
	"github.com/docker/swarm-v2/log"
	"github.com/spf13/cobra"
	"golang.org/x/net/context"
)

var (
	listCmd = &cobra.Command{
		Use:     "list",
		Short:   "List volumes",
		Aliases: []string{"ls"},
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			c, err := common.Dial(cmd)
			if err != nil {
				return err
			}
			r, err := c.ListVolumes(common.Context(cmd), &api.ListVolumesRequest{})
			if err != nil {
				return err
			}

			w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
			defer func() {
				// Ignore flushing errors - there's nothing we can do.
				_ = w.Flush()
			}()
			common.PrintHeader(w, "ID", "Name", "Driver", "Options")
			for _, v := range r.Volumes {
				spec := &v.Spec
				name := spec.Annotations.Name
				driverName := spec.DriverConfig.Name
				driverOptions := spec.DriverConfig.Options

				// TODO(amitshukla): Right now we only implement the happy path
				// and don't have any proper error handling whatsover.
				// Instead of aborting, we should display what we can of the Volume.
				if name == "" || v.ID == "" || driverName == "" {
					log.G(ctx).Fatalf("Malformed volume: %v", v)
				}

				// TODO(amitshukla): Improve formatting of driver options
				fmt.Fprintf(w, "%s\t%s\t%s\t%v\n",
					v.ID,
					name,
					driverName,
					driverOptions,
				)
			}
			return nil
		},
	}
)
