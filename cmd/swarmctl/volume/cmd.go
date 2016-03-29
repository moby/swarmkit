package volume

import "github.com/spf13/cobra"

var (
	// Cmd exposes the top-level volume command
	Cmd = &cobra.Command{
		Use:   "volume",
		Short: "Volume management",
	}
)

func init() {
	Cmd.AddCommand(
		inspectCmd,
		listCmd,
		createCmd,
		removeCmd,
	)
}
