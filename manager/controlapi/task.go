package controlapi

import (
	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/manager/state/store"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
)

// GetTask returns a Task given a TaskID.
// - Returns `InvalidArgument` if TaskID is not provided.
// - Returns `NotFound` if the Task is not found.
func (s *Server) GetTask(ctx context.Context, request *api.GetTaskRequest) (*api.GetTaskResponse, error) {
	if request.TaskID == "" {
		return nil, grpc.Errorf(codes.InvalidArgument, errInvalidArgument.Error())
	}

	var task *api.Task
	s.store.View(func(tx store.ReadTx) {
		task = store.GetTask(tx, request.TaskID)
	})
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

	err := s.store.Update(func(tx store.Tx) error {
		return store.DeleteTask(tx, request.TaskID)
	})
	if err != nil {
		if err == store.ErrNotExist {
			return nil, grpc.Errorf(codes.NotFound, "task %s not found", request.TaskID)
		}
		return nil, err
	}
	return &api.RemoveTaskResponse{}, nil
}

// ListTasks returns a list of all tasks.
func (s *Server) ListTasks(ctx context.Context, request *api.ListTasksRequest) (*api.ListTasksResponse, error) {
	var (
		tasks []*api.Task
		err   error
	)
	s.store.View(func(tx store.ReadTx) {
		switch {
		case request.Filters != nil && len(request.Filters.Names) > 0:
			tasks, err = store.FindTasks(tx, buildFilters(store.ByName, request.Filters.Names))
		case request.Filters != nil && len(request.Filters.IDPrefixes) > 0:
			tasks, err = store.FindTasks(tx, buildFilters(store.ByIDPrefix, request.Filters.IDPrefixes))
		case request.Filters != nil && len(request.Filters.ServiceIDs) > 0:
			tasks, err = store.FindTasks(tx, buildFilters(store.ByServiceID, request.Filters.ServiceIDs))
		case request.Filters != nil && len(request.Filters.NodeIDs) > 0:
			tasks, err = store.FindTasks(tx, buildFilters(store.ByNodeID, request.Filters.NodeIDs))
		default:
			tasks, err = store.FindTasks(tx, store.All)
		}
	})
	if err != nil {
		return nil, err
	}
	return &api.ListTasksResponse{
		Tasks: tasks,
	}, nil
}
