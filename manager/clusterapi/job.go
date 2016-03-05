package clusterapi

import (
	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/identity"
	"github.com/docker/swarm-v2/state"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
)

// TODO(vieux): refactor validation once we have more objects to validate
func validateJobSpecMeta(m *api.Meta) error {
	if m == nil {
		return grpc.Errorf(codes.InvalidArgument, "meta: required in job spec")
	}
	if m.Name == "" {
		return grpc.Errorf(codes.InvalidArgument, "meta: name must be provided")
	}
	return nil
}

func validateJobSpecSource(s *api.Source) error {
	if s == nil {
		return grpc.Errorf(codes.InvalidArgument, "source: required in job spec")
	}
	image := s.GetImage()
	if image == nil {
		return grpc.Errorf(codes.Unimplemented, "source: invalid source type. only image is supposed")
	}
	if image.Reference == "" {
		return grpc.Errorf(codes.InvalidArgument, "source: image reference must be provided")
	}
	return nil
}

func validateJobSpecOrchestration(o *api.JobSpec_Orchestration) error {
	if o == nil {
		return grpc.Errorf(codes.InvalidArgument, "orchestration: required in job spec")
	}
	job := o.GetJob()
	if job == nil {
		return grpc.Errorf(codes.InvalidArgument, "orchestration: job must be provided")
	}
	switch job.(type) {
	case *api.JobSpec_Orchestration_Batch:
		return grpc.Errorf(codes.Unimplemented, "orchestration: batch is not supported")
	case *api.JobSpec_Orchestration_Cron:
		return grpc.Errorf(codes.Unimplemented, "orchestration: cron is not supported")
	case *api.JobSpec_Orchestration_Global:
		return grpc.Errorf(codes.Unimplemented, "orchestration: global is not supported")
	case *api.JobSpec_Orchestration_Service:
		if o.GetService() == nil {
			return grpc.Errorf(codes.InvalidArgument, "orchestration: service must be provided")
		}
	}
	return nil
}

func validateJobSpec(spec *api.JobSpec) error {
	if spec == nil {
		return grpc.Errorf(codes.InvalidArgument, errInvalidArgument.Error())
	}
	if err := validateJobSpecMeta(spec.Meta); err != nil {
		return err
	}
	if err := validateJobSpecSource(spec.Source); err != nil {
		return err
	}
	if err := validateJobSpecOrchestration(spec.Orchestration); err != nil {
		return err
	}
	return nil
}

// CreateJob creates and return a Job based on the provided JobSpec.
// - Returns `InvalidArgument` if the JobSpec is malformed.
// - Returns `Unimplemented` if the JobSpec references unimplemented features.
// - Returns `AlreadyExists` if the JobID conflicts.
// - Returns an error if the creation fails.
func (s *Server) CreateJob(ctx context.Context, request *api.CreateJobRequest) (*api.CreateJobResponse, error) {
	if err := validateJobSpec(request.Spec); err != nil {
		return nil, err
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

// RemoveJob removes a Job referenced by JobID.
// - Returns `InvalidArgument` if JobID is not provided.
// - Returns `NotFound` if the Job is not found.
// - Returns an error if the deletion fails.
func (s *Server) RemoveJob(ctx context.Context, request *api.RemoveJobRequest) (*api.RemoveJobResponse, error) {
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
	return &api.RemoveJobResponse{}, nil
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
