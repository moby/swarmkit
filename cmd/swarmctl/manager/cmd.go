package managers

import "github.com/spf13/cobra"

var (
	// Cmd exposes the top-level managers command.
	Cmd = &cobra.Command{
		Use:   "manager",
		Short: "Manager membership management",
	}
)

func init() {
	Cmd.AddCommand(
		removeCmd,
	)
}
