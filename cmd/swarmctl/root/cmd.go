package root

import "github.com/spf13/cobra"

var (
	// Cmds exposes the list of top-level node command.
	Cmds = []*cobra.Command{
		upCmd,
		downCmd,
		updateCmd,
		diffCmd,
	}
)
