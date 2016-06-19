package flagparser

import (
	"fmt"
	"strings"

	"github.com/docker/swarmkit/api"
	"github.com/spf13/pflag"
)

func parseHostConfig(flags *pflag.FlagSet, spec *api.ServiceSpec) error {
	if err := parseLogConfig(flags, spec); err != nil {
		return err
	}

	return nil
}

func parseLogConfig(flags *pflag.FlagSet, spec *api.ServiceSpec) error {
	if !flags.Changed("log-driver") {
		return nil
	}

	spec.Task.Hostconfig.Logconfig = &api.ContainerHostConfigSpec_LogConfig{}

	driver, err := flags.GetString("log-driver")
	if err != nil {
		return err
	}
	spec.Task.Hostconfig.Logconfig.Type = driver

	if flags.Changed("log-opt") {
		options, err := flags.GetStringSlice("log-opt")
		if err != nil {
			return err
		}

		spec.Task.Hostconfig.Logconfig.Config = map[string]string{}
		for _, opt := range options {
			parts := strings.SplitN(opt, "=", 2)
			if len(parts) != 2 {
				return fmt.Errorf("malformed option: %s", opt)
			}
			spec.Task.Hostconfig.Logconfig.Config[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
		}
	}
	return nil
}
