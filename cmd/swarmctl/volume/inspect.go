package volume

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"
)

var (
	inspectCmd = &cobra.Command{
		Use:   "inspect",
		Short: "Inspect a volume",
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
	inspectCmd.Flags().String("name", "", "Volume name")
}
