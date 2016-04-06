package network

import (
	"fmt"

	"github.com/docker/swarm-v2/api"
	"golang.org/x/net/context"
)

// GetNetwork tries to query for a network as an ID and if it can't be
// found tries to query as a name. If the name query returns exactly
// one entry then it is returned to the caller. Otherwise an error is
// returned.
func GetNetwork(ctx context.Context, c api.ClusterClient, input string) (*api.Network, error) {
	// GetJob to match via full ID.
	rg, err := c.GetNetwork(ctx, &api.GetNetworkRequest{NetworkID: input})
	if err != nil {
		// If any error (including NotFound), ListJobs to match via ID prefix and full name.
		rl, err := c.ListNetworks(ctx, &api.ListNetworksRequest{Options: &api.ListOptions{Query: input}})
		if err != nil {
			return nil, err
		}

		if len(rl.Networks) == 0 {
			return nil, fmt.Errorf("network %s not found", input)
		}

		if l := len(rl.Networks); l > 1 {
			return nil, fmt.Errorf("network %s is ambigious (%d matches found)", input, l)
		}

		return rl.Networks[0], nil
	}

	return rg.Network, nil
}
