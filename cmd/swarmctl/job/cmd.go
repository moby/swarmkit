package job

import "github.com/spf13/cobra"

var (
	// Cmd exposes the top-level job command.
	Cmd = &cobra.Command{
		Use:   "job",
		Short: "Job management",
	}
)

func init() {
	Cmd.AddCommand(
		createCmd,
		lsCmd,
	)
}
