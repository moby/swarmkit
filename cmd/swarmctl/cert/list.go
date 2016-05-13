package cert

import (
	"fmt"
	"os"
	"sort"
	"text/tabwriter"

	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/cmd/swarmctl/common"
	"github.com/spf13/cobra"
)

type certsByID []*api.RegisteredCertificate

func (m certsByID) Len() int {
	return len(m)
}

func (m certsByID) Swap(i, j int) {
	m[i], m[j] = m[j], m[i]
}

func (m certsByID) Less(i, j int) bool {
	return m[i].ID < m[j].ID
}

var (
	listCmd = &cobra.Command{
		Use:   "ls",
		Short: "List registered certificates",
		RunE: func(cmd *cobra.Command, args []string) error {
			flags := cmd.Flags()

			quiet, err := flags.GetBool("quiet")
			if err != nil {
				return err
			}

			pending, err := flags.GetBool("pending")
			if err != nil {
				return err
			}

			c, err := common.Dial(cmd)
			if err != nil {
				return err
			}

			request := &api.ListRegisteredCertificatesRequest{}

			if pending {
				request.State = []api.IssuanceState{api.IssuanceStatePending}
			}

			r, err := c.ListRegisteredCertificates(common.Context(cmd), request)
			if err != nil {
				return err
			}

			var output func(c *api.RegisteredCertificate)

			if !quiet {
				w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
				defer func() {
					// Ignore flushing errors - there's nothing we can do.
					_ = w.Flush()
				}()

				// TODO(abronan): include member name and raft cluster it belongs to
				common.PrintHeader(w, "ID", "CN", "Role", "Status")
				output = func(c *api.RegisteredCertificate) {
					fmt.Fprintf(w, "%s\t%s\t%s\t%s\n",
						c.ID,
						c.CN,
						c.Role,
						c.Status.State,
					)
				}
			} else {
				output = func(c *api.RegisteredCertificate) { fmt.Println(c.ID) }
			}

			sortedCerts := certsByID(r.Certificates)
			sort.Sort(sortedCerts)
			for _, c := range sortedCerts {
				output(c)
			}
			return nil
		},
	}
)

func init() {
	listCmd.Flags().BoolP("quiet", "q", false, "Only display IDs")
	listCmd.Flags().BoolP("pending", "p", false, "Only list pending requests")
}
