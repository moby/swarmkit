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
	task := s.store.Task(request.TaskID)
	if task == nil {
		return nil, grpc.Errorf(codes.NotFound, "task %s not found", request.TaskID)
	}
	return &api.GetTaskResponse{
		Task: task,
	}, nil
}

// DeleteTask deletes a Task referenced by TaskID.
// - Returns `InvalidArgument` if TaskID is not provided.
// - Returns `NotFound` if the Task is not found.
// - Returns an error if the deletion fails.
func (s *Server) DeleteTask(ctx context.Context, request *api.DeleteTaskRequest) (*api.DeleteTaskResponse, error) {
	if request.TaskID == "" {
		return nil, grpc.Errorf(codes.InvalidArgument, errInvalidArgument.Error())
	}
	if err := s.store.DeleteTask(request.TaskID); err != nil {
		if err == state.ErrNotExist {
			return nil, grpc.Errorf(codes.NotFound, "task %s not found", request.TaskID)
		}
		return nil, err
	}
	return &api.DeleteTaskResponse{}, nil
}

// ListTasks returns a list of all tasks.
func (s *Server) ListTasks(ctx context.Context, request *api.ListTasksRequest) (*api.ListTasksResponse, error) {
	return &api.ListTasksResponse{
		Tasks: s.store.Tasks(),
	}, nil
}
