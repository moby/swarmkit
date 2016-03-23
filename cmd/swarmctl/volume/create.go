package volume

import (
	"errors"
	"fmt"
	"strings"

	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/cmd/swarmctl/common"
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
			driverName, err := flags.GetString("driver")
			if err != nil {
				return err
			}

			driver := new(api.Driver)
			driver.Name = driverName
			opts, err := cmd.Flags().GetStringSlice("opts")
			if err != nil {
				return err
			}

			driver.Options = map[string]string{}
			for _, opt := range opts {
				optPair := strings.Split(opt, "=")
				if len(optPair) != 2 {
					return fmt.Errorf("Malformed opts: %s", opt)
				}
				driver.Options[optPair[0]] = optPair[1]
			}

			spec := &api.VolumeSpec{
				Meta: api.Meta{
					Name: name,
				},
				DriverConfiguration: driver,
			}

			c, err := common.Dial(cmd)
			if err != nil {
				return err
			}
			r, err := c.CreateVolume(common.Context(cmd), &api.CreateVolumeRequest{Spec: spec})
			if err != nil {
				return err
			}
			fmt.Println(r.Volume.ID)
			return nil
		},
	}
)

func init() {
	createCmd.Flags().String("name", "", "Volume name")
	createCmd.Flags().String("driver", "", "Volume driver")
	createCmd.Flags().StringSlice("opts", []string{}, "Volume driver options")
}
