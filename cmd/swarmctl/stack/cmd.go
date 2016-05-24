package stack

import "github.com/spf13/cobra"

var (
	// Cmd exposes the top-level stack command.
	Cmd = &cobra.Command{
		Use:   "stack",
		Short: "Stack management",
	}
)

func init() {
	// Cmds exposes the list of top-level stack command.
	Cmd.AddCommand(
		printCmd,
		listCmd,
		deployCmd,
		removeCmd,

		diffCmd,
	)
}
