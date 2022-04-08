package flagparser

import (
	"github.com/moby/swarmkit/v2/api"
	"github.com/moby/swarmkit/v2/cmd/swarmctl/common"
	"github.com/moby/swarmkit/v2/cmd/swarmctl/network"
	"github.com/spf13/cobra"
)

func parseNetworks(cmd *cobra.Command, spec *api.ServiceSpec, c api.ControlClient) error {
	flags := cmd.Flags()

	if !flags.Changed("network") {
		return nil
	}
	input, err := flags.GetString("network")
	if err != nil {
		return err
	}

	n, err := network.GetNetwork(common.Context(cmd), c, input)
	if err != nil {
		return err
	}

	spec.Task.Networks = []*api.NetworkAttachmentConfig{
		{
			Target: n.ID,
		},
	}
	return nil
}
