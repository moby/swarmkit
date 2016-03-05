package clusterapi

import (
	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/state"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
)

// CreateTask creates and return a Task based on the provided JobSpec.
// TODO(aluzzardi): Not implemented.
func (s *Server) CreateTask(ctx context.Context, request *api.CreateTaskRequest) (*api.CreateTaskResponse, error) {
	return nil, grpc.Errorf(codes.Unimplemented, errNotImplemented.Error())
}

// GetTask returns a Task given a TaskID.
// - Returns `InvalidArgument` if TaskID is not provided.
// - Returns `NotFound` if the Task is not found.
func (s *Server) GetTask(ctx context.Context, request *api.GetTaskRequest) (*api.GetTaskResponse, error) {
	if request.TaskID == "" {
		return nil, grpc.Errorf(codes.InvalidArgument, errInvalidArgument.Error())
	}

	var task *api.Task
	err := s.store.View(func(tx state.ReadTx) error {
		task = tx.Tasks().Get(request.TaskID)
		return nil
	})
	if err != nil {
		return nil, err
	}
	if task == nil {
		return nil, grpc.Errorf(codes.NotFound, "task %s not found", request.TaskID)
	}
	return &api.GetTaskResponse{
		Task: task,
	}, nil
}

// RemoveTask removes a Task referenced by TaskID.
// - Returns `InvalidArgument` if TaskID is not provided.
// - Returns `NotFound` if the Task is not found.
// - Returns an error if the deletion fails.
func (s *Server) RemoveTask(ctx context.Context, request *api.RemoveTaskRequest) (*api.RemoveTaskResponse, error) {
	if request.TaskID == "" {
		return nil, grpc.Errorf(codes.InvalidArgument, errInvalidArgument.Error())
	}

	err := s.store.Update(func(tx state.Tx) error {
		return tx.Tasks().Delete(request.TaskID)
	})
	if err != nil {
		if err == state.ErrNotExist {
			return nil, grpc.Errorf(codes.NotFound, "task %s not found", request.TaskID)
		}
		return nil, err
	}
	return &api.RemoveTaskResponse{}, nil
}

// ListTasks returns a list of all tasks.
func (s *Server) ListTasks(ctx context.Context, request *api.ListTasksRequest) (*api.ListTasksResponse, error) {
	var tasks []*api.Task
	err := s.store.View(func(tx state.ReadTx) error {
		var err error

		tasks, err = tx.Tasks().Find(state.All)
		return err
	})
	if err != nil {
		return nil, err
	}
	return &api.ListTasksResponse{
		Tasks: tasks,
	}, nil
}
