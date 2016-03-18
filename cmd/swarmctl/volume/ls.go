package volume

import "github.com/spf13/cobra"

var (
	lsCmd = &cobra.Command{
		Use:     "list",
		Short:   "List volumes",
		Aliases: []string{"ls"},
		RunE: func(cmd *cobra.Command, args []string) error {
			// TODO: Send it to the Manager thru grpc

			return nil
		},
	}
)
