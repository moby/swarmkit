package node

import (
	"errors"
	"fmt"

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
	id := common.LookupID(common.Context(cmd), c, args[0])
	r, err := c.GetNode(common.Context(cmd), &api.GetNodeRequest{
		NodeID: id,
	})
	if err != nil {
		return err
	}
	spec := r.Node.Spec
	if spec == nil {
		spec = &api.NodeSpec{}
	}

	if spec.Availability == availability {
		return fmt.Errorf("node %s is already paused", args[0])
	}

	spec.Availability = availability

	_, err = c.UpdateNode(common.Context(cmd), &api.UpdateNodeRequest{
		NodeID: r.Node.ID,
		Spec:   spec,
	})

	if err != nil {
		return err
	}

	return nil
}
