package node

import (
	"errors"
	"fmt"

	"github.com/docker/swarm-v2/cmd/swarmctl/common"
	"github.com/docker/swarm-v2/pb/docker/cluster/api"
	objectspb "github.com/docker/swarm-v2/pb/docker/cluster/objects"
	specspb "github.com/docker/swarm-v2/pb/docker/cluster/specs"
	"github.com/spf13/cobra"
	"golang.org/x/net/context"
)

var (
	errNoChange = errors.New("availability was already set to the requested value")
)

func changeNodeAvailability(cmd *cobra.Command, args []string, availability specspb.NodeSpec_Availability) error {
	if len(args) == 0 {
		return errors.New("missing Node ID")
	}

	c, err := common.Dial(cmd)
	if err != nil {
		return err
	}
	node, err := getNode(common.Context(cmd), c, args[0])
	if err != nil {
		return err
	}
	spec := node.Spec
	if spec == nil {
		spec = &specspb.NodeSpec{}
	}

	if spec.Availability == availability {
		return fmt.Errorf("node %s is already paused", args[0])
	}

	spec.Availability = availability

	_, err = c.UpdateNode(common.Context(cmd), &api.UpdateNodeRequest{
		NodeID: node.ID,
		Spec:   spec,
	})

	if err != nil {
		return err
	}

	return nil
}

func getNode(ctx context.Context, c api.ClusterClient, input string) (*objectspb.Node, error) {
	// GetNode to match via full ID.
	rg, err := c.GetNode(ctx, &api.GetNodeRequest{NodeID: input})
	if err != nil {
		// If any error (including NotFound), ListJobs to match via ID prefix and full name.
		rl, err := c.ListNodes(ctx, &api.ListNodesRequest{Options: &api.ListOptions{Query: input}})
		if err != nil {
			return nil, err
		}

		if len(rl.Nodes) == 0 {
			return nil, fmt.Errorf("node %s not found", input)
		}

		if l := len(rl.Nodes); l > 1 {
			return nil, fmt.Errorf("node %s is ambigious (%d matches found)", input, l)
		}

		return rl.Nodes[0], nil
	}
	return rg.Node, nil
}
