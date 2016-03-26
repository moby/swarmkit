package node

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"text/tabwriter"

	"github.com/docker/go-units"
	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/cmd/swarmctl/common"
	"github.com/spf13/cobra"
)

var (
	inspectCmd = &cobra.Command{
		Use:   "inspect <node ID>",
		Short: "Inspect a node",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return errors.New("node ID missing")
			}
			c, err := common.Dial(cmd)
			if err != nil {
				return err
			}

			node, err := getNode(common.Context(cmd), c, args[0])
			if err != nil {
				return err
			}
			w := tabwriter.NewWriter(os.Stdout, 8, 8, 8, ' ', 0)
			defer func() {
				// Ignore flushing errors - there's nothing we can do.
				_ = w.Flush()
			}()
			spec := node.Spec
			if spec == nil {
				spec = &api.NodeSpec{}
			}
			desc := node.Description
			if desc == nil {
				desc = &api.NodeDescription{}
			}

			common.FprintfIfNotEmpty(w, "ID\t: %s\n", node.ID)
			common.FprintfIfNotEmpty(w, "Name\t: %s\n", spec.Meta.Name)
			common.FprintfIfNotEmpty(w, "Hostname\t: %s\n", node.Description.Hostname)
			fmt.Fprintf(w, "Platform\t: %s-%s\n", desc.Platform.OS, desc.Platform.Architecture)

			fmt.Fprintln(w, "Status:\t")
			common.FprintfIfNotEmpty(w, "  State\t: %s\n", node.Status.State.String())
			common.FprintfIfNotEmpty(w, "  Message\t: %s\n", node.Status.Message)
			common.FprintfIfNotEmpty(w, "  Availability\t: %s\n", spec.Availability.String())

			fmt.Fprintln(w, "Resources:\t")
			common.FprintfIfNotEmpty(w, "  CPUs\t: %s\n", strconv.Itoa(int(desc.Resources.CPU)))
			common.FprintfIfNotEmpty(w, "  Memory\t: %s\n", units.BytesSize(float64(desc.Resources.Memory)))
			return nil
		},
	}
)
