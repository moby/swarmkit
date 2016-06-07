package cluster

import (
	"errors"
	"fmt"

	"github.com/docker/swarmkit/cmd/swarmctl/common"
	"github.com/docker/swarmkit/spec"
	"github.com/spf13/cobra"
)

var (
	diffCmd = &cobra.Command{
		Use:   "diff <cluster name>",
		Short: "Diff a cluster",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return errors.New("cluster name missing")
			}

			flags := cmd.Flags()

			if !flags.Changed("file") {
				return errors.New("--file is mandatory")
			}

			context, err := flags.GetInt("context")
			if err != nil {
				return err
			}

			c, err := common.Dial(cmd)
			if err != nil {
				return err
			}

			cluster, err := getCluster(common.Context(cmd), c, args[0])
			if err != nil {
				return err
			}
			remoteSpec := cluster.Spec

			localCluster, err := readClusterConfig(flags)
			if err != nil {
				return err
			}

			remoteCluster := &spec.ClusterConfig{}
			remoteCluster.FromProto(&remoteSpec)
			diff, err := localCluster.Diff(context, "remote", "local", remoteCluster)
			if err != nil {
				return err
			}
			fmt.Print(diff)
			return nil
		},
	}
)

func init() {
	diffCmd.Flags().StringP("file", "f", "", "Spec to use")
	diffCmd.Flags().IntP("context", "c", 3, "lines of copied context (default 3)")
}
