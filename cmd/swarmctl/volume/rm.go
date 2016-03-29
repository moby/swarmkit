package volume

// TODO(amitshukla): rename this file to remove.go and ls.Cmd to removeCmd - will do this change in a separate PR

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"
)

var (
	rmCmd = &cobra.Command{
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
