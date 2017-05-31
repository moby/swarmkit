package cluster

import (
	"fmt"

	"golang.org/x/net/context"

	"github.com/docker/swarmkit/api"
)

func getCluster(ctx context.Context, c api.ControlClient, input string) (*api.Cluster, error) {
	rg, err := c.GetCluster(ctx, &api.GetClusterRequest{ClusterID: input})
	if err == nil {
		return rg.Cluster, nil
	}
	rl, err := c.ListClusters(ctx,
		&api.ListClustersRequest{
			Filters: &api.ListClustersRequest_Filters{
				Names: []string{input},
			},
		},
	)
	if err != nil {
		return nil, err
	}

	if len(rl.Clusters) == 0 {
		return nil, fmt.Errorf("cluster %s not found", input)
	}

	if l := len(rl.Clusters); l > 1 {
		var match *api.Cluster
		// Find and return the unique match or fail out
		for _, c := range rl.Clusters {
			if c.Spec.Annotations.Name == input {
				if match != nil {
					match = nil
					break
				}
				match = c
			}
		}
		if match != nil {
			return match, nil
		}
		return nil, fmt.Errorf("cluster %s is ambiguous (%d matches found)", input, l)
	}

	return rl.Clusters[0], nil
}
