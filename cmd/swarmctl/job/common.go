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
func getJob(ctx context.Context, c api.ClusterClient, prefix string) (*api.Job, error) {
	r, err := c.ListJobs(ctx, &api.ListJobsRequest{Options: &api.ListOptions{Prefix: prefix}})
	if err != nil {
		return nil, err
	}

	if len(r.Jobs) == 0 {
		return nil, fmt.Errorf("job %s not found", prefix)
	}

	if len(r.Jobs) > 1 {
		return nil, fmt.Errorf("job %s is ambigious", prefix)
	}

	return r.Jobs[0], nil
}
