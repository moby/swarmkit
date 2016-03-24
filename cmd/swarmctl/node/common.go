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
		spec = &api.NodeSpec{}
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

func getNode(ctx context.Context, c api.ClusterClient, prefix string) (*api.Node, error) {
	r, err := c.ListNodes(ctx, &api.ListNodesRequest{Options: &api.ListOptions{Prefix: prefix}})
	if err != nil {
		return nil, err
	}

	if len(r.Nodes) == 0 {
		return nil, fmt.Errorf("node %s not found", prefix)
	}

	if len(r.Nodes) > 1 {
		return nil, fmt.Errorf("node %s is ambigious", prefix)
	}

	return r.Nodes[0], nil
}
