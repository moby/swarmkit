package cluster

import (
	"errors"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/docker/swarmkit/api"
	"github.com/docker/swarmkit/cmd/swarmctl/common"
	"github.com/docker/swarmkit/protobuf/ptypes"
	"github.com/spf13/cobra"
)

func printClusterSummary(cluster *api.Cluster) {
	w := tabwriter.NewWriter(os.Stdout, 8, 8, 8, ' ', 0)
	defer w.Flush()

	common.FprintfIfNotEmpty(w, "ID\t: %s\n", cluster.ID)
	common.FprintfIfNotEmpty(w, "Name\t: %s\n", cluster.Spec.Annotations.Name)
	if len(cluster.Spec.AcceptancePolicy.Policies) > 0 {
		fmt.Fprintf(w, "Acceptance Policies:\n")
		for _, policy := range cluster.Spec.AcceptancePolicy.Policies {
			fmt.Fprintf(w, "  Role\t: %v\n", policy.Role)
			fmt.Fprintf(w, "    Autoaccept\t: %v\n", policy.Autoaccept)
			if policy.Secret != nil {
				fmt.Fprintf(w, "    Secret\t: %v\n", string(policy.Secret.Data))
			}
		}
	}
	fmt.Fprintf(w, "Orchestration settings:\n")
	fmt.Fprintf(w, "  Task history entries: %d\n", cluster.Spec.Orchestration.TaskHistoryRetentionLimit)

	heartbeatPeriod, err := ptypes.Duration(cluster.Spec.Dispatcher.HeartbeatPeriod)
	if err == nil {
		fmt.Fprintf(w, "Dispatcher settings:\n")
		fmt.Fprintf(w, "  Dispatcher heartbeat period: %s\n", heartbeatPeriod.String())
	}

	if cluster.Spec.CAConfig.NodeCertExpiry != nil {
		fmt.Fprintf(w, "Certificate Authority settings:\n")
		clusterDuration, err := ptypes.Duration(cluster.Spec.CAConfig.NodeCertExpiry)
		if err != nil {
			fmt.Fprintf(w, "  Certificate Validity Duration: [ERROR PARSING DURATION]\n")
		} else {
			fmt.Fprintf(w, "  Certificate Validity Duration: %s\n", clusterDuration.String())
		}
		fmt.Fprintf(w, "  External CA URLs: %s\n", cluster.Spec.CAConfig.ExternalCAURLs)
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
