package managers

import "github.com/spf13/cobra"

var (
	// Cmd exposes the top-level managers command.
	Cmd = &cobra.Command{
		Use:   "managers",
		Short: "Manager membership management and introspection",
	}
)

func init() {
	Cmd.AddCommand(
		inspectCmd,
		listCmd,
	)
}
