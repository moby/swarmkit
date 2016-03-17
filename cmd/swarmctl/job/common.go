package job

import (
	"os"

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
