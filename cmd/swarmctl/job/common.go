package job

import (
	"fmt"
	"os"

	"golang.org/x/net/context"

	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/spec"
	flag "github.com/spf13/pflag"
)

func readServiceConfig(flags *flag.FlagSet) (*spec.ServiceConfig, error) {
	path, err := flags.GetString("file")
	if err != nil {
		return nil, err
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	service := &spec.ServiceConfig{}
	if err := service.Read(file); err != nil {
		return nil, err
	}

	return service, nil
}

func getJob(ctx context.Context, c api.ClusterClient, input string) (*api.Job, error) {
	// GetJob to match via full ID.
	rg, err := c.GetJob(ctx, &api.GetJobRequest{JobID: input})
	if err != nil {
		// If any error (including NotFound), ListJobs to match via ID prefix and full name.
		rl, err := c.ListJobs(ctx, &api.ListJobsRequest{Options: &api.ListOptions{Query: input}})
		if err != nil {
			return nil, err
		}

		if len(rl.Jobs) == 0 {
			return nil, fmt.Errorf("job %s not found", input)
		}

		if l := len(rl.Jobs); l > 1 {
			return nil, fmt.Errorf("job %s is ambigious (%d matches found)", input, l)
		}

		return rl.Jobs[0], nil
	}
	return rg.Job, nil
}
