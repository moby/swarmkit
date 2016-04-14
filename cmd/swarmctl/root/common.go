package root

import (
	"os"

	"golang.org/x/net/context"

	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/spec"
	flag "github.com/spf13/pflag"
)

func getJobByName(ctx context.Context, c api.ClusterClient, name string) *api.Job {
	r, err := c.ListJobs(ctx, &api.ListJobsRequest{
		Options: &api.ListOptions{
			Query: name,
		},
	})
	if err != nil {
		return nil
	}

	if len(r.Jobs) != 1 {
		return nil
	}

	if r.Jobs[0].Spec.Meta.Name != name {
		return nil
	}

	return r.Jobs[0]
}

func readSpec(flags *flag.FlagSet) (*spec.Spec, error) {
	path, err := flags.GetString("file")
	if err != nil {
		return nil, err
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	s := &spec.Spec{}
	if err := s.Read(file); err != nil {
		return nil, err
	}

	return s, nil
}
