package node

import (
	"errors"
	"fmt"
	"os"
	"text/tabwriter"

	"reflect"

	"github.com/docker/swarmkit/api"
	"github.com/docker/swarmkit/ca"
	"github.com/docker/swarmkit/cmd/swarmctl/common"
	"github.com/spf13/cobra"
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

			c, err := common.Dial(cmd)
			if err != nil {
				return err
			}
			r, err := c.ListNodes(common.Context(cmd), &api.ListNodesRequest{})
			if err != nil {
				return err
			}

			var output func(n *api.Node)

			cluster, err := getCluster(common.Context(cmd), c)
			if err != nil {
				return err
			}
			clusterIssuer, err := ca.IssuerFromAPIRootCA(&cluster.RootCA)
			if err != nil {
				return err
			}
			desiredTLSInfo := &api.NodeTLSInfo{
				CertIssuerPublicKey: clusterIssuer.PublicKey,
				CertIssuerSubject:   clusterIssuer.Subject,
				TrustRoot:           cluster.RootCA.CACert,
			}

			if !quiet {
				w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
				defer func() {
					// Ignore flushing errors - there's nothing we can do.
					_ = w.Flush()
				}()
				common.PrintHeader(w, "ID", "Name", "Membership", "Status", "Availability", "Manager Status", "TLS Status")
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
					tlsStatus := "OUTDATED"
					if n.Description == nil || n.Description.TLSInfo == nil {
						tlsStatus = "UNKNOWN"
					} else if reflect.DeepEqual(n.Description.TLSInfo, desiredTLSInfo) {
						tlsStatus = "CURRENT"
					}

					fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
						n.ID,
						name,
						membership,
						n.Status.State.String(),
						availability,
						reachability,
						tlsStatus,
					)
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
}
