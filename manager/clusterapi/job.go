package clusterapi

import (
	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/identity"
	"github.com/docker/swarm-v2/state"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
)

// CreateJob creates and return a Job based on the provided JobSpec.
// - Returns `InvalidArgument` if the JobSpec is malformed.
// - Returns `Unimplemented` if the JobSpec references unimplemented features.
// - Returns `AlreadyExists` if the JobID conflicts.
// - Returns an error if the creation fails.
func (s *Server) CreateJob(ctx context.Context, request *api.CreateJobRequest) (*api.CreateJobResponse, error) {
	// TODO(aluzzardi): We need to validate that JobSpec.
	if request.Spec == nil {
		return nil, grpc.Errorf(codes.InvalidArgument, errInvalidArgument.Error())
	}

	// TODO(aluzzardi): Consider using `Name` as a primary key to handle
	// duplicate creations. See #65
	job := &api.Job{
		ID:   identity.NewID(),
		Spec: request.Spec,
	}

	err := s.store.Update(func(tx state.Tx) error {
		return tx.Jobs().Create(job)
	})
	if err != nil {
		return nil, err
	}

	return &api.CreateJobResponse{
		Job: job,
	}, nil
}

// GetJob returns a Job given a JobID.
// - Returns `InvalidArgument` if JobID is not provided.
// - Returns `NotFound` if the Job is not found.
func (s *Server) GetJob(ctx context.Context, request *api.GetJobRequest) (*api.GetJobResponse, error) {
	if request.JobID == "" {
		return nil, grpc.Errorf(codes.InvalidArgument, errInvalidArgument.Error())
	}

	var job *api.Job
	err := s.store.View(func(tx state.ReadTx) error {
		job = tx.Jobs().Get(request.JobID)
		return nil
	})
	if err != nil {
		return nil, err
	}
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

	err := s.store.Update(func(tx state.Tx) error {
		return tx.Jobs().Delete(request.JobID)
	})
	if err != nil {
		if err == state.ErrNotExist {
			return nil, grpc.Errorf(codes.NotFound, "job %s not found", request.JobID)
		}
		return nil, err
	}
	return &api.DeleteJobResponse{}, nil
}

// ListJobs returns a list of all jobs.
func (s *Server) ListJobs(ctx context.Context, request *api.ListJobsRequest) (*api.ListJobsResponse, error) {
	var jobs []*api.Job
	err := s.store.View(func(tx state.ReadTx) error {
		var err error

		jobs, err = tx.Jobs().Find(state.All)
		return err
	})
	if err != nil {
		return nil, err
	}
	return &api.ListJobsResponse{
		Jobs: jobs,
	}, nil
}
