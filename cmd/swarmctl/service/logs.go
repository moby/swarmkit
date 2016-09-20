package service

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/docker/swarmkit/api"
	"github.com/docker/swarmkit/cmd/swarmctl/common"
	"github.com/docker/swarmkit/protobuf/ptypes"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
)

var (
	logsCmd = &cobra.Command{
		Use:     "logs",
		Short:   "Obtain log output from a service",
		Aliases: []string{"log"},
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			if len(args) > 0 {
				return errors.New("logs takes no arguments")
			}

			c, err := common.DialConn(cmd)
			if err != nil {
				return err
			}

			client := api.NewLogsClient(c)
			stream, err := client.SubscribeLogs(ctx, &api.SubscribeLogsRequest{})
			if err != nil {
				return errors.Wrap(err, "failed to subscribe to logs")
			}

			for {
				subscribeMsg, err := stream.Recv()
				if err != nil {
					return errors.Wrap(err, "failed receiving stream message")
				}

				for _, msg := range subscribeMsg.Messages {
					ts, _ := ptypes.Timestamp(msg.Timestamp)

					out := os.Stdout
					if msg.Stream == api.LogStreamStderr {
						out = os.Stderr
					}

					fmt.Fprintf(out, "%s %s.%s.%s|", ts.Format(time.RFC3339), msg.Context.NodeID, msg.Context.ServiceID, msg.Context.TaskID)
					out.Write(msg.Data) // assume new line?
				}
			}
			return nil
		},
	}
)
