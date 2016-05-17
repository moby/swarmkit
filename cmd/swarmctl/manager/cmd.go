package managers

import "github.com/spf13/cobra"

var (
	// Cmd exposes the top-level managers command.
	Cmd = &cobra.Command{
		Use:   "manager",
		Short: "Manager membership management and introspection",
	}
)

func init() {
	Cmd.AddCommand(
		inspectCmd,
		listCmd,
		removeCmd,
	)
}
