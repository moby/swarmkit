package service

import (
	"fmt"
	"io"
	"os"

	"github.com/docker/swarmkit/api"
	"github.com/docker/swarmkit/cmd/swarmctl/common"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"golang.org/x/net/context"
)

var (
	logsCmd = &cobra.Command{
		Use:     "logs <service ID...>",
		Short:   "Obtain log output from a service",
		Aliases: []string{"log"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return errors.New("missing service IDs")
			}

			follow, err := cmd.Flags().GetBool("follow")
			if err != nil {
				return err
			}
			noStdout, err := cmd.Flags().GetBool("no-stdout")
			if err != nil {
				return err
			}
			noStderr, err := cmd.Flags().GetBool("no-stderr")
			if err != nil {
				return err
			}

			ctx := context.Background()
			conn, err := common.DialConn(cmd)
			if err != nil {
				return err
			}

			c := api.NewControlClient(conn)
			r := common.NewResolver(cmd, c)

			serviceIDs := []string{}
			for _, arg := range args {
				service, err := getService(common.Context(cmd), c, arg)
				if err != nil {
					return err
				}
				serviceIDs = append(serviceIDs, service.ID)
			}

			stdstreams := []api.LogStream{}
			if !noStdout {
				stdstreams = append(stdstreams, api.LogStreamStdout)
			}
			if !noStderr {
				stdstreams = append(stdstreams, api.LogStreamStderr)
			}
			client := api.NewLogsClient(conn)
			stream, err := client.SubscribeLogs(ctx, &api.SubscribeLogsRequest{
				Selector: &api.LogSelector{
					ServiceIDs: serviceIDs,
				},
				Options: &api.LogSubscriptionOptions{
					Follow:  follow,
					Streams: stdstreams,
				},
			})
			if err != nil {
				return errors.Wrap(err, "failed to subscribe to logs")
			}

			for {
				log, err := stream.Recv()
				if err == io.EOF {
					return nil
				}
				if err != nil {
					return errors.Wrap(err, "failed receiving stream message")
				}

				for _, msg := range log.Messages {
					out := os.Stdout
					if msg.Stream == api.LogStreamStderr {
						out = os.Stderr
					}

					fmt.Fprintf(out, "%s@%s❯ ",
						r.Resolve(api.Task{}, msg.Context.TaskID),
						r.Resolve(api.Node{}, msg.Context.NodeID),
					)
					out.Write(msg.Data) // assume new line?
				}
			}
		},
	}
)

func init() {
	logsCmd.Flags().BoolP("follow", "f", false, "Follow log output")
	logsCmd.Flags().Bool("no-stdout", false, "Ignore stdout messages")
	logsCmd.Flags().Bool("no-stderr", false, "Ignore stderr messages")
}
