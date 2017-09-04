package service

import (
	"bufio"
	"errors"
	"os"

	"github.com/docker/swarmkit/api"
	"github.com/docker/swarmkit/cmd/swarmctl/common"
	"github.com/spf13/cobra"
)

var (
	execCmd = &cobra.Command{
		Use:   "exec <service ID>",
		Short: "Run a command in a running service",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return errors.New("service ID missing")
			}

			if len(args) > 1 {
				return errors.New("exec command takes exactly 1 argument (FOR NOW, we will need to take the executable!)")
			}

			c, err := common.Dial(cmd)
			if err != nil {
				return err
			}

			service, err := getService(common.Context(cmd), c, args[0])
			if err != nil {
				return err
			}

			r, err := c.ListTasks(common.Context(cmd),
				&api.ListTasksRequest{
					Filters: &api.ListTasksRequest_Filters{
						ServiceIDs: []string{service.ID},
					},
				})
			if err != nil {
				return err
			}

			var currentTask *api.Task
			for _, t := range r.Tasks {
				if t.Status.State == api.TaskStateRunning {
					currentTask = t
					break
				}
			}

			if currentTask == nil {
				// TODO(fntlnz): better error message
				return errors.New("cannot find a running task")
			}

			attachCl, err := c.Attach(common.Context(cmd))
			if err != nil {
				return err
			}
			containerId := currentTask.Status.GetContainer().ContainerID

			go func() {
				for {
					data, err := attachCl.Recv()

					if err != nil {
						continue
					}
					os.Stdout.Write(data.Message)
				}
			}()

			stdinr := bufio.NewReader(os.Stdin)

			for {
				curLine, _ := stdinr.ReadByte()
				attachCl.Send(&api.TaskExecStream{
					Containerid: containerId,
					Message:     []byte{curLine},
				})

			}

		},
	}
)
