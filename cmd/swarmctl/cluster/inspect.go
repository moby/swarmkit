package cluster

import (
	"errors"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/ca"
	"github.com/docker/swarm-v2/cmd/swarmctl/common"
	"github.com/spf13/cobra"
)

func printClusterSummary(cluster *api.Cluster) {
	w := tabwriter.NewWriter(os.Stdout, 8, 8, 8, ' ', 0)
	defer w.Flush()

	common.FprintfIfNotEmpty(w, "ID\t: %s\n", cluster.ID)
	common.FprintfIfNotEmpty(w, "Name\t: %s\n", cluster.Spec.Annotations.Name)
	if cluster.Spec.AcceptancePolicy.Autoaccept != nil {
		fmt.Fprintf(w, "Acceptance strategy:\n")
		fmt.Fprintf(w, "  Autoaccept agents\t: %v\n", cluster.Spec.AcceptancePolicy.Autoaccept[ca.AgentRole])
		fmt.Fprintf(w, "  Autoaccept managers\t: %v\n", cluster.Spec.AcceptancePolicy.Autoaccept[ca.ManagerRole])
	}
}

var (
	inspectCmd = &cobra.Command{
		Use:   "inspect <cluster name>",
		Short: "Inspect a cluster",
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

			printClusterSummary(cluster)

			return nil
		},
	}
)
