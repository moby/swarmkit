package root

import (
	"os"

	"github.com/docker/swarm-v2/spec"
	flag "github.com/spf13/pflag"
)

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
