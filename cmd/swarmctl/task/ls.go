package task

import "github.com/spf13/cobra"

var (
	lsCmd = &cobra.Command{
		Use:   "ls",
		Short: "List tasks",
		RunE: func(cmd *cobra.Command, args []string) error {
			return nil
		},
	}
)

func init() {
	Cmd.AddCommand(lsCmd)
}
