package cluster

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"text/tabwriter"

	"github.com/moby/swarmkit/swarmd/cmd/swarmctl/common"
	"github.com/moby/swarmkit/v2/api"
	"github.com/spf13/cobra"
)

func printClusterSummary(cluster *api.Cluster) {
	w := tabwriter.NewWriter(os.Stdout, 8, 8, 8, ' ', 0)
	defer w.Flush()

	common.FprintfIfNotEmpty(w, "ID\t: %s\n", cluster.Id)
	common.FprintfIfNotEmpty(w, "Name\t: %s\n", cluster.GetSpec().GetAnnotations().GetName())
	fmt.Fprintln(w, "Orchestration settings:")
	fmt.Fprintf(w, "  Task history entries: %d\n", cluster.Spec.GetOrchestration().GetTaskHistoryRetentionLimit())

	// AsDuration cannot fail, so keep the "only print a valid period" behaviour
	// by testing the timestamp explicitly.
	if hb := cluster.Spec.GetDispatcher().GetHeartbeatPeriod(); hb.IsValid() {
		heartbeatPeriod := hb.AsDuration()
		fmt.Fprintln(w, "Dispatcher settings:")
		fmt.Fprintf(w, "  Dispatcher heartbeat period: %s\n", heartbeatPeriod.String())
	}

	fmt.Fprintln(w, "Certificate Authority settings:")
	if cluster.Spec.GetCaConfig().GetNodeCertExpiry() != nil {
		if !cluster.Spec.GetCaConfig().GetNodeCertExpiry().IsValid() {
			fmt.Fprintln(w, "  Certificate Validity Duration: [ERROR PARSING DURATION]")
		} else {
			clusterDuration := cluster.Spec.GetCaConfig().GetNodeCertExpiry().AsDuration()
			fmt.Fprintf(w, "  Certificate Validity Duration: %s\n", clusterDuration.String())
		}
	}
	if len(cluster.Spec.GetCaConfig().GetExternalCas()) > 0 {
		fmt.Fprintln(w, "  External CAs:")
		for _, ca := range cluster.Spec.GetCaConfig().GetExternalCas() {
			fmt.Fprintf(w, "    %s: %s\n", ca.Protocol, ca.Url)
		}
	}

	fmt.Fprintln(w, "  Join Tokens:")
	fmt.Fprintln(w, "    Worker:", cluster.RootCa.GetJoinTokens().GetWorker())
	fmt.Fprintln(w, "    Manager:", cluster.RootCa.GetJoinTokens().GetManager())

	if cluster.Spec.GetTaskDefaults().GetLogDriver() != nil {
		fmt.Fprintf(w, "Default Log Driver\t: %s\n", cluster.Spec.GetTaskDefaults().GetLogDriver().Name)
		var keys []string

		if len(cluster.Spec.GetTaskDefaults().GetLogDriver().Options) != 0 {
			for k := range cluster.Spec.GetTaskDefaults().GetLogDriver().Options {
				keys = append(keys, k)
			}
			sort.Strings(keys)

			for _, k := range keys {
				v := cluster.Spec.GetTaskDefaults().GetLogDriver().Options[k]
				if v != "" {
					fmt.Fprintf(w, "  %s\t: %s\n", k, v)
				} else {
					fmt.Fprintf(w, "  %s\t\n", k)
				}
			}
		}
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

			if len(args) > 1 {
				return errors.New("inspect command takes exactly 1 argument")
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
