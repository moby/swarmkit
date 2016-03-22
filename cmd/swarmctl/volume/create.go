package volume

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"
)

var (
	createCmd = &cobra.Command{
		Use:   "create",
		Short: "Create a volume",
		RunE: func(cmd *cobra.Command, args []string) error {
			flags := cmd.Flags()
			if !flags.Changed("name") || !flags.Changed("driver") {
				return errors.New("--name and --driver are mandatory")
			}
			name, err := flags.GetString("name")
			if err != nil {
				return err
			}
			driver, err := flags.GetString("driver")
			if err != nil {
				return err
			}
			opts, err := flags.GetString("opts")
			if err != nil {
				return err
			}

			fmt.Printf("Volume Name = %s, driver = %s, options = %s\n", name, driver, opts)
			// TODO: Send it to the Manager thru grpc

			return nil
		},
	}
)

func init() {
	createCmd.Flags().String("name", "", "Volume name")
	createCmd.Flags().String("driver", "", "Volume driver")
	createCmd.Flags().String("opts", "", "Volume driver options")
}
