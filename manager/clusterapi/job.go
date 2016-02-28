package clusterapi

import (
	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/state"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
)

// CreateJob creates and return a Job based on the provided JobSpec.
// TODO(aluzzardi): Not implemented.
func (s *Server) CreateJob(ctx context.Context, request *api.CreateJobRequest) (*api.CreateJobResponse, error) {
	return nil, grpc.Errorf(codes.Unimplemented, errNotImplemented.Error())
}

// GetJob returns a Job given a JobID.
// - Returns `InvalidArgument` if JobID is not provided.
// - Returns `NotFound` if the Job is not found.
func (s *Server) GetJob(ctx context.Context, request *api.GetJobRequest) (*api.GetJobResponse, error) {
	if request.JobID == "" {
		return nil, grpc.Errorf(codes.InvalidArgument, errInvalidArgument.Error())
	}
	job := s.store.Job(request.JobID)
	if job == nil {
		return nil, grpc.Errorf(codes.NotFound, "job %s not found", request.JobID)
	}
	return &api.GetJobResponse{
		Job: job,
	}, nil
}

// UpdateJob updates a Job referenced by JobID with the given JobSpec.
// TODO(aluzzardi): Not implemented.
func (s *Server) UpdateJob(ctx context.Context, request *api.UpdateJobRequest) (*api.UpdateJobResponse, error) {
	return nil, grpc.Errorf(codes.Unimplemented, errNotImplemented.Error())
}

// DeleteJob deletes a Job referenced by JobID.
// - Returns `InvalidArgument` if JobID is not provided.
// - Returns `NotFound` if the Job is not found.
// - Returns an error if the deletion fails.
func (s *Server) DeleteJob(ctx context.Context, request *api.DeleteJobRequest) (*api.DeleteJobResponse, error) {
	if request.JobID == "" {
		return nil, grpc.Errorf(codes.InvalidArgument, errInvalidArgument.Error())
	}
	if err := s.store.DeleteJob(request.JobID); err != nil {
		if err == state.ErrNotExist {
			return nil, grpc.Errorf(codes.NotFound, "job %s not found", request.JobID)
		}
		return nil, err
	}
	return &api.DeleteJobResponse{}, nil
}

// ListJobs returns a list of all jobs.
func (s *Server) ListJobs(ctx context.Context, request *api.ListJobsRequest) (*api.ListJobsResponse, error) {
	return &api.ListJobsResponse{
		Jobs: s.store.Jobs(),
	}, nil
}
