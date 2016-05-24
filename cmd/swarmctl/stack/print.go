package stack

import (
	"errors"
	"os"

	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/cmd/swarmctl/common"
	"github.com/docker/swarm-v2/spec"
	"github.com/spf13/cobra"
)

var (
	printCmd = &cobra.Command{
		Use:   "print <stack>",
		Short: "Print a stack",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return errors.New("stack name missing")
			}
			stack := args[0]

			c, err := common.Dial(cmd)
			if err != nil {
				return err
			}

			r, err := c.ListServices(common.Context(cmd), &api.ListServicesRequest{})
			if err != nil {
				return err
			}

			servicespecs := []*api.ServiceSpec{}

			for _, j := range r.Services {
				if j.Spec.Annotations.Labels["stack"] == stack {
					servicespecs = append(servicespecs, &j.Spec)
				}
			}

			// Volumes
			v, err := c.ListVolumes(common.Context(cmd), &api.ListVolumesRequest{})
			if err != nil {
				return err
			}

			volumespecs := []*api.VolumeSpec{}

			for _, j := range v.Volumes {
				if j.Spec.Annotations.Labels["stack"] == stack {
					volumespecs = append(volumespecs, &j.Spec)
				}
			}

			remoteSpec := &spec.Spec{
				Name:     stack,
				Services: make(map[string]*spec.ServiceConfig),
				Volumes:  make(map[string]*spec.VolumeConfig),
			}
			remoteSpec.FromServiceSpecs(servicespecs)
			remoteSpec.FromVolumeSpecs(volumespecs)

			remoteSpec.Write(os.Stdout)
			return nil
		},
	}
)
