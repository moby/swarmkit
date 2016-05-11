package cluster

import (
	"errors"
	"fmt"

	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/ca"
	"github.com/docker/swarm-v2/cmd/swarmctl/common"
	"github.com/spf13/cobra"
)

var (
	updateCmd = &cobra.Command{
		Use:   "update <cluster name>",
		Short: "Update a cluster",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return errors.New("cluster name missing")
			}

			c, err := common.Dial(cmd)
			if err != nil {
				return err
			}

			cluster, err := getCluster(common.Context(cmd), c, args[0])
			if err != nil {
				return err
			}

			flags := cmd.Flags()
			var spec *api.ClusterSpec

			if flags.Changed("file") {
				cluster, err := readClusterConfig(flags)
				if err != nil {
					return err
				}
				spec = cluster.ToProto()
			} else { // TODO(vieux): support or error on both file.
				spec = &cluster.Spec

				if flags.Changed("autoaccept") {
					autoaccept, err := flags.GetStringSlice("autoaccept")
					if err != nil {
						return err
					}
					spec.AcceptancePolicy.Autoaccept = make(map[string]bool)

					for _, role := range autoaccept {
						switch role {
						case "agent":
							spec.AcceptancePolicy.Autoaccept[ca.AgentRole] = true
						case "manager":
							spec.AcceptancePolicy.Autoaccept[ca.ManagerRole] = true
						default:
							return fmt.Errorf("unrecognized role %s", role)
						}
					}
				}
			}

			r, err := c.UpdateCluster(common.Context(cmd), &api.UpdateClusterRequest{
				ClusterID:      cluster.ID,
				ClusterVersion: &cluster.Meta.Version,
				Spec:           spec,
			})
			if err != nil {
				return err
			}
			fmt.Println(r.Cluster.ID)
			return nil
		},
	}
)

func init() {
	updateCmd.Flags().StringP("file", "f", "", "Spec to use")
	// TODO(aaronl): Acceptance policy will change later.
	updateCmd.Flags().StringSlice("autoaccept", nil, "Roles to automatically issue certificates for")
}
