package secret

import (
	"errors"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/moby/swarmkit/swarmd/cmd/swarmctl/common"
	"github.com/moby/swarmkit/v2/api"
	"github.com/spf13/cobra"
)

func printSecretSummary(secret *api.Secret) {
	w := tabwriter.NewWriter(os.Stdout, 8, 8, 8, ' ', 0)
	defer w.Flush()

	common.FprintfIfNotEmpty(w, "ID\t: %s\n", secret.Id)
	common.FprintfIfNotEmpty(w, "Name\t: %s\n", secret.GetSpec().GetAnnotations().GetName())
	if len(secret.GetSpec().GetAnnotations().GetLabels()) > 0 {
		fmt.Fprintln(w, "Labels\t")
		for k, v := range secret.GetSpec().GetAnnotations().GetLabels() {
			fmt.Fprintf(w, "  %s\t: %s\n", k, v)
		}
	}

	common.FprintfIfNotEmpty(w, "Created\t: %s\n", common.TimestampString(secret.Meta.CreatedAt))
}

var (
	inspectCmd = &cobra.Command{
		Use:   "inspect <secret ID or name>",
		Short: "Inspect a secret",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				return errors.New("inspect command takes a single secret ID or name")
			}

			client, err := common.Dial(cmd)
			if err != nil {
				return err
			}

			secret, err := getSecret(common.Context(cmd), client, args[0])
			if err != nil {
				return err
			}

			printSecretSummary(secret)
			return nil
		},
	}
)
