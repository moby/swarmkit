package main

import (
	"fmt"

	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/cmd/swarmctl/common"
	"github.com/docker/swarm-v2/spec"
	"github.com/spf13/cobra"
)

var (
	deployCmd = &cobra.Command{
		Use:   "deploy",
		Short: "Deploy an app",
		RunE: func(cmd *cobra.Command, args []string) error {
			path, err := cmd.Flags().GetString("file")
			if err != nil {
				return err
			}

			s, err := spec.ReadFrom(path)
			if err != nil {
				return err
			}

			if s.Version == 2 {
				fmt.Println("WARNING: v2 format is only partially supported, please update to v3")
			}

			c, err := common.Dial(cmd)
			if err != nil {
				return err
			}
			for _, jobSpec := range s.JobSpecs() {
				r, err := c.CreateJob(common.Context(cmd), &api.CreateJobRequest{Spec: jobSpec})
				if err != nil {
					return err
				}
				fmt.Printf("%s: %s\n", jobSpec.Meta.Name, r.Job.ID)
			}
			return nil
		},
	}
)

func init() {
	deployCmd.Flags().StringP("file", "f", "docker.yml", "Spec file to deploy")
}
