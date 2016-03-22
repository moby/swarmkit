package volume

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"
)

var (
	rmCmd = &cobra.Command{
		Use:     "remove <Volume name>",
		Short:   "Remove a volume",
		Aliases: []string{"rm"},
		RunE: func(cmd *cobra.Command, args []string) error {
			flags := cmd.Flags()
			if !flags.Changed("name") {
				return errors.New("--name is required")
			}
			name, err := flags.GetString("name")
			if err != nil {
				return err
			}

			fmt.Printf("Volume Name = %s\n", name)

			// TODO: Send it to the Manager thru grpc

			return nil
		},
	}
)

func init() {
	rmCmd.Flags().String("name", "", "Volume name")
}
