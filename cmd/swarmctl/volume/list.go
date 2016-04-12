package volume

import (
	"fmt"
	"os"
	"text/tabwriter"

	log "github.com/Sirupsen/logrus"
	"github.com/docker/swarm-v2/cmd/swarmctl/common"
	"github.com/docker/swarm-v2/pb/docker/cluster/api"
	specspb "github.com/docker/swarm-v2/pb/docker/cluster/specs"
	"github.com/spf13/cobra"
)

var (
	listCmd = &cobra.Command{
		Use:     "list",
		Short:   "List volumes",
		Aliases: []string{"ls"},
		RunE: func(cmd *cobra.Command, args []string) error {
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
			fmt.Fprintln(w, "ID\tName\tDriver\tOptions")
			for _, v := range r.Volumes {
				spec := v.Spec
				if spec == nil {
					spec = &specspb.VolumeSpec{}
				}
				name := spec.Meta.Name

				// TODO(amitshukla): Right now we only implement the happy path
				// and don't have any proper error handling whatsover.
				// Instead of aborting, we should display what we can of the Volume.
				if name == "" || v.ID == "" {
					log.Fatalf("Malformed volume: %v", v)
				}

				driverName := ""
				driverOptions := map[string]string{}
				if spec.DriverConfiguration != nil {
					driverName = spec.DriverConfiguration.Name
					driverOptions = spec.DriverConfiguration.Options
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
