package cluster

import (
	"fmt"
	"os"

	"golang.org/x/net/context"

	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/spec"
	flag "github.com/spf13/pflag"
)

func readClusterConfig(flags *flag.FlagSet) (*spec.ClusterConfig, error) {
	path, err := flags.GetString("file")
	if err != nil {
		return nil, err
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	cluster := &spec.ClusterConfig{}
	if err := cluster.Read(file); err != nil {
		return nil, err
	}

	return cluster, nil
}

func getCluster(ctx context.Context, c api.ControlClient, input string) (*api.Cluster, error) {
	rl, err := c.ListClusters(ctx, &api.ListClustersRequest{
		Filters: &api.ListClustersRequest_Filters{
			Names: []string{input},
		},
	})
	if err != nil {
		return nil, err
	}

	if len(rl.Clusters) == 0 {
		return nil, fmt.Errorf("cluster %s not found", input)
	}

	if l := len(rl.Clusters); l > 1 {
		return nil, fmt.Errorf("cluster %s is ambigious (%d matches found)", input, l)
	}

	return rl.Clusters[0], nil
}
