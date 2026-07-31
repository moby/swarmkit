package config

import (
	"errors"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/moby/swarmkit/swarmd/cmd/swarmctl/common"
	"github.com/moby/swarmkit/v2/api"
	"github.com/spf13/cobra"
)

func printConfigSummary(config *api.Config) {
	w := tabwriter.NewWriter(os.Stdout, 8, 8, 8, ' ', 0)
	defer w.Flush()

	common.FprintfIfNotEmpty(w, "ID\t: %s\n", config.Id)
	common.FprintfIfNotEmpty(w, "Name\t: %s\n", config.GetSpec().GetAnnotations().GetName())
	if len(config.GetSpec().GetAnnotations().GetLabels()) > 0 {
		fmt.Fprintln(w, "Labels\t")
		for k, v := range config.GetSpec().GetAnnotations().GetLabels() {
			fmt.Fprintf(w, "  %s\t: %s\n", k, v)
		}
	}

	common.FprintfIfNotEmpty(w, "Created\t: %s\n", common.TimestampString(config.Meta.CreatedAt))

	fmt.Print(w, "Payload:\n\n")
	fmt.Println(w, config.Spec.GetData())
}

var (
	inspectCmd = &cobra.Command{
		Use:   "inspect <config ID or name>",
		Short: "Inspect a config",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				return errors.New("inspect command takes a single config ID or name")
			}

			client, err := common.Dial(cmd)
			if err != nil {
				return err
			}

			config, err := getConfig(common.Context(cmd), client, args[0])
			if err != nil {
				return err
			}

			printConfigSummary(config)
			return nil
		},
	}
)
