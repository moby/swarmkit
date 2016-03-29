package volume

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"
)

var (
	removeCmd = &cobra.Command{
		Use:     "remove <volume ID>",
		Short:   "Remove a volume",
		Aliases: []string{"rm"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return errors.New("volume ID is missing")
			}

			fmt.Printf("Volume ID = %s\n", args[0])

			// TODO(amitshukla): Send it to the Manager thru grpc

			return nil
		},
	}
)
