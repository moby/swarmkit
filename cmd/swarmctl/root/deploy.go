package root

import (
	"fmt"
	"reflect"

	"github.com/docker/swarm-v2/cmd/swarmctl/common"
	"github.com/docker/swarm-v2/pb/docker/cluster/api"
	objectspb "github.com/docker/swarm-v2/pb/docker/cluster/objects"
	"github.com/spf13/cobra"
)

var (
	deployCmd = &cobra.Command{
		Use:   "deploy",
		Short: "Deploy an app",
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := readSpec(cmd.Flags())
			if err != nil {
				return err
			}

			c, err := common.Dial(cmd)
			if err != nil {
				return err
			}

			r, err := c.ListJobs(common.Context(cmd), &api.ListJobsRequest{})
			if err != nil {
				return err
			}

			jobs := map[string]*objectspb.Job{}

			for _, j := range r.Jobs {
				if j.Spec.Meta.Labels["namespace"] == s.Namespace {
					jobs[j.Spec.Meta.Name] = j
				}
			}

			for _, jobSpec := range s.JobSpecs() {
				if job, ok := jobs[jobSpec.Meta.Name]; ok && !reflect.DeepEqual(job.Spec, jobSpec) {
					r, err := c.UpdateJob(common.Context(cmd), &api.UpdateJobRequest{JobID: job.ID, Spec: jobSpec})
					if err != nil {
						fmt.Printf("%s: %v", jobSpec.Meta.Name, err)
						continue
					}
					fmt.Printf("%s: %s - UPDATED\n", jobSpec.Meta.Name, r.Job.ID)
					delete(jobs, jobSpec.Meta.Name)
				} else if !ok {
					r, err := c.CreateJob(common.Context(cmd), &api.CreateJobRequest{Spec: jobSpec})
					if err != nil {
						fmt.Printf("%s: %v", jobSpec.Meta.Name, err)
						continue
					}
					fmt.Printf("%s: %s - CREATED\n", jobSpec.Meta.Name, r.Job.ID)
				} else {
					// nothing to update
					delete(jobs, jobSpec.Meta.Name)
				}
			}

			for _, job := range jobs {
				_, err := c.RemoveJob(common.Context(cmd), &api.RemoveJobRequest{JobID: job.ID})
				if err != nil {

					return err
				}
				fmt.Printf("%s: %s - REMOVED\n", job.Spec.Meta.Name, job.ID)
			}
			return nil
		},
	}
)

func init() {
	deployCmd.Flags().StringP("file", "f", "docker.yml", "Spec file to deploy")
}
