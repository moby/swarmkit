package cert

import "github.com/spf13/cobra"

var (
	// Cmd exposes the top-level managers command.
	Cmd = &cobra.Command{
		Use:   "cert",
		Short: "Certificate management",
	}
)

func init() {
	Cmd.AddCommand(
		listCmd,
		acceptCmd,
		rejectCmd,
	)
}
