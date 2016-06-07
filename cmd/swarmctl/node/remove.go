package node

import (
	"errors"

	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/cmd/swarmctl/common"
	"github.com/spf13/cobra"
)

var (
	removeCmd = &cobra.Command{
		Use:     "remove <node ID>",
		Short:   "Remove a node",
		Aliases: []string{"rm"},
		RunE: func(cmd *cobra.Command, args []string) error {
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

			_, err = c.RemoveNode(common.Context(cmd), &api.RemoveNodeRequest{
				NodeID:      node.ID,
				NodeVersion: &node.Meta.Version,
			})

			return err
		},
	}
)
