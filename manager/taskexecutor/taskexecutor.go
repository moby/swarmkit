package taskexecutor

import (
	"github.com/docker/swarmkit/api"
	"github.com/docker/swarmkit/manager/controlapi"
)

type TaskExecServer struct {
	stream *controlapi.ServerTaskExecChannels
}

func New(stream *controlapi.ServerTaskExecChannels) *TaskExecServer {
	return &TaskExecServer{
		stream: stream,
	}
}

func (tes *TaskExecServer) Attach(stream api.TaskExec_AttachServer) error {

	go func() {
		for {
			data, err := stream.Recv()
			if err != nil {
				continue
			}
			tes.stream.Out(data.Containerid) <- data.Message
		}
	}()

	for {
		ins := tes.stream.Ins()

		for containerid, c := range ins {
			select {
			case data := <-c:
				stream.Send(&api.TaskExecStream{
					Message:     data,
					Containerid: containerid,
				})
			case <-stream.Context().Done():
				return nil
			}
		}
	}
}
