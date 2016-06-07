package flagparser

import (
	"fmt"
	"strings"

	"github.com/docker/swarmkit/api"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// AddServiceFlags add all supported service flags to the flagset.
func AddServiceFlags(flags *pflag.FlagSet) {
	flags.String("name", "", "service name")
	flags.StringSlice("label", nil, "service label (key=value)")

	flags.String("mode", "replicated", "one of replicated, global")
	flags.Uint64("instances", 1, "number of instances for the service")

	flags.String("image", "", "container image")
	flags.StringSlice("args", nil, "container args")
	flags.StringSlice("env", nil, "container env")

	flags.StringSlice("ports", nil, "ports")
	flags.String("network", "", "network name")

	flags.StringSlice("reservation", nil, "amount of reserved resources (key=value e.g. --reservation memory=512m --reservation cpu=0.5)")
	flags.StringSlice("limit", nil, "resources limit (key=value e.g. --limit memory=512m --limit cpu=0.5)")

	flags.Uint64("update-parallelism", 0, "task update parallelism (0 = all at once)")
	flags.String("update-delay", "0s", "delay between task updates (0s = none)")

	flags.String("restart-condition", "any", "condition to restart the task (any, failure, none)")
	flags.String("restart-delay", "5s", "delay between task restarts")
	flags.Uint64("restart-max-attempts", 0, "maximum number of restart attempts (0 = unlimited)")
	flags.String("restart-window", "0s", "time window to evaluate restart attempts (0 = unbound)")

	flags.StringSlice("constraint", nil, "Placement constraint (node.labels.key==value)")
}

// Merge merges a flagset into a service spec.
func Merge(cmd *cobra.Command, spec *api.ServiceSpec, c api.ControlClient) error {
	flags := cmd.Flags()

	if flags.Changed("name") {
		name, err := flags.GetString("name")
		if err != nil {
			return err
		}
		spec.Annotations.Name = name
	}

	if flags.Changed("label") {
		labels, err := flags.GetStringSlice("label")
		if err != nil {
			return err
		}
		spec.Annotations.Labels = map[string]string{}
		for _, l := range labels {
			parts := strings.SplitN(l, "=", 2)
			if len(parts) != 2 {
				return fmt.Errorf("malformed label: %s", l)
			}
			spec.Annotations.Labels[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
		}
	}

	if err := parseMode(flags, spec); err != nil {
		return err
	}

	if err := parseContainer(flags, spec); err != nil {
		return err
	}

	if err := parseResource(flags, spec); err != nil {
		return err
	}

	if err := parsePorts(flags, spec); err != nil {
		return err
	}

	if err := parseNetworks(cmd, spec, c); err != nil {
		return err
	}

	if err := parseRestart(flags, spec); err != nil {
		return err
	}

	if err := parseUpdate(flags, spec); err != nil {
		return err
	}

	if err := parsePlacement(flags, spec); err != nil {
		return err
	}

	return nil
}
