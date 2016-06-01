package node

import (
	"errors"
	"fmt"

	"golang.org/x/net/context"

	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/cmd/swarmctl/common"
	"github.com/spf13/cobra"
)

var (
	errNoChange = errors.New("availability was already set to the requested value")
)

func changeNodeAvailability(cmd *cobra.Command, args []string, availability api.NodeSpec_Availability) error {
	if len(args) == 0 {
		return errors.New("missing node ID")
	}

	c, err := common.Dial(cmd)
	if err != nil {
		return err
	}
	node, err := getNode(common.Context(cmd), c, args[0])
	if err != nil {
		return err
	}
	spec := &node.Spec

	if spec.Availability == availability {
		return errNoChange
	}

	spec.Availability = availability

	_, err = c.UpdateNode(common.Context(cmd), &api.UpdateNodeRequest{
		NodeID:      node.ID,
		NodeVersion: &node.Meta.Version,
		Spec:        spec,
	})

	if err != nil {
		return err
	}

	return nil
}

func changeNodeAcceptance(cmd *cobra.Command, args []string, acceptance api.NodeSpec_Acceptance) error {
	if len(args) == 0 {
		return errors.New("missing node ID")
	}

	c, err := common.Dial(cmd)
	if err != nil {
		return err
	}
	node, err := getNode(common.Context(cmd), c, args[0])
	if err != nil {
		return err
	}
	spec := &node.Spec

	if spec.Acceptance == acceptance {
		return errNoChange
	}

	spec.Acceptance = acceptance

	_, err = c.UpdateNode(common.Context(cmd), &api.UpdateNodeRequest{
		NodeID:      node.ID,
		NodeVersion: &node.Meta.Version,
		Spec:        spec,
	})

	if err != nil {
		return err
	}

	return nil
}

func getNode(ctx context.Context, c api.ControlClient, input string) (*api.Node, error) {
	// GetNode to match via full ID.
	rg, err := c.GetNode(ctx, &api.GetNodeRequest{NodeID: input})
	if err != nil {
		// If any error (including NotFound), ListServices to match via ID prefix and full name.
		rl, err := c.ListNodes(ctx, &api.ListNodesRequest{
			Filters: &api.ListNodesRequest_Filters{
				Names: []string{input},
			},
		})
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
