package node

import "github.com/spf13/cobra"

var (
	removeCmd = &cobra.Command{
		Use:   "remove <node ID>",
		Short: "Remove a node",
		RunE:  removeNode,
	}
)
