package stack

import (
	"errors"
	"fmt"

	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/cmd/swarmctl/common"
	"github.com/docker/swarm-v2/spec"
	"github.com/spf13/cobra"
)

var (
	diffCmd = &cobra.Command{
		Use:   "diff <name>",
		Short: "Diff a stack",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return errors.New("stack name missing")
			}
			stack := args[0]

			c, err := common.Dial(cmd)
			if err != nil {
				return err
			}

			flags := cmd.Flags()

			context, err := flags.GetInt("context")
			if err != nil {
				return err
			}

			r, err := c.ListServices(common.Context(cmd), &api.ListServicesRequest{})
			if err != nil {
				return err
			}

			localSpec, err := readSpec(flags)
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
				Version:  localSpec.Version,
				Name:     stack,
				Services: make(map[string]*spec.ServiceConfig),
				Volumes:  make(map[string]*spec.VolumeConfig),
			}
			remoteSpec.FromServiceSpecs(servicespecs)
			remoteSpec.FromVolumeSpecs(volumespecs)

			diff, err := localSpec.Diff(context, "remote", "local", remoteSpec)
			if err != nil {
				return err
			}
			fmt.Print(diff)
			return nil
		},
	}
)

func init() {
	diffCmd.Flags().StringP("file", "f", "docker.yml", "Spec file to diff")
	diffCmd.Flags().IntP("context", "c", 3, "lines of copied context (default 3)")
}
