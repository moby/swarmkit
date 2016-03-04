package clusterapi

import (
	"testing"

	"github.com/docker/swarm-v2/api"
	"github.com/stretchr/testify/assert"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
)

func createSpec(name, image string, instances int64) *api.JobSpec {
	return &api.JobSpec{
		Meta: &api.Meta{
			Name: name,
		},
		Source: &api.Source{
			Source: &api.Source_Image{
				Image: &api.ImageSpec{
					Reference: image,
				},
			},
		},
		Orchestration: &api.JobSpec_Orchestration{
			Job: &api.JobSpec_Orchestration_Service{
				Service: &api.JobSpec_ServiceJob{
					Instances: instances,
				},
			},
		},
	}
}

func createJob(ts *testServer, name, image string, instances int64) *api.Job {
	spec := createSpec(name, image, instances)
	r, _ := ts.Client.CreateJob(context.Background(), &api.CreateJobRequest{Spec: spec})
	return r.Job
}

func TestValidateJobSpecMeta(t *testing.T) {
	var m *api.Meta
	err := validateJobSpecMeta(m)
	assert.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, grpc.Code(err))

	m = &api.Meta{}
	err = validateJobSpecMeta(m)
	assert.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, grpc.Code(err))

	m.Name = "name"
	err = validateJobSpecMeta(m)
	assert.NoError(t, err)
}

func TestValidateJobSpecSource(t *testing.T) {
	var s *api.Source
	err := validateJobSpecSource(s)
	assert.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, grpc.Code(err))

	s = &api.Source{}
	err = validateJobSpecSource(s)
	assert.Error(t, err)
	assert.Equal(t, codes.Unimplemented, grpc.Code(err))

	s.Source = &api.Source_Image{
		Image: &api.ImageSpec{},
	}

	err = validateJobSpecSource(s)
	assert.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, grpc.Code(err))

	s.GetImage().Reference = "image"
	err = validateJobSpecSource(s)
	assert.NoError(t, err)
}

func TestValidateJobSpecOrchestration(t *testing.T) {
	var o *api.JobSpec_Orchestration
	err := validateJobSpecOrchestration(o)
	assert.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, grpc.Code(err))

	o = &api.JobSpec_Orchestration{}
	err = validateJobSpecOrchestration(o)
	assert.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, grpc.Code(err))

	o.Job = &api.JobSpec_Orchestration_Batch{}
	err = validateJobSpecOrchestration(o)
	assert.Error(t, err)
	assert.Equal(t, codes.Unimplemented, grpc.Code(err))

	o.Job = &api.JobSpec_Orchestration_Service{}
	err = validateJobSpecOrchestration(o)
	assert.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, grpc.Code(err))

	o.Job = &api.JobSpec_Orchestration_Service{
		Service: &api.JobSpec_ServiceJob{
			Instances: 1,
		},
	}
	err = validateJobSpecOrchestration(o)
	assert.NoError(t, err)
}

func TestValidateJobSpec(t *testing.T) {
	var spec *api.JobSpec
	err := validateJobSpec(spec)
	assert.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, grpc.Code(err))

	spec = &api.JobSpec{
		Meta: &api.Meta{
			Name: "name",
		},
		Source: &api.Source{
			Source: &api.Source_Image{
				Image: &api.ImageSpec{
					Reference: "image",
				},
			},
		},
		Orchestration: &api.JobSpec_Orchestration{
			Job: &api.JobSpec_Orchestration_Service{
				Service: &api.JobSpec_ServiceJob{
					Instances: 1,
				},
			},
		},
	}
	err = validateJobSpec(spec)
	assert.NoError(t, err)
}

func TestCreateJob(t *testing.T) {
	ts := newTestServer(t)
	_, err := ts.Client.CreateJob(context.Background(), &api.CreateJobRequest{})
	assert.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, grpc.Code(err))

	spec := createSpec("name", "image", 1)
	r, err := ts.Client.CreateJob(context.Background(), &api.CreateJobRequest{Spec: spec})
	assert.NoError(t, err)
	assert.NotEmpty(t, r.Job.ID)
}

func TestGetJob(t *testing.T) {
	ts := newTestServer(t)
	_, err := ts.Client.GetJob(context.Background(), &api.GetJobRequest{})
	assert.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, grpc.Code(err))

	job := createJob(ts, "name", "image", 1)
	r, err := ts.Client.GetJob(context.Background(), &api.GetJobRequest{JobID: job.ID})
	assert.NoError(t, err)
	assert.Equal(t, job, r.Job)
}

func TestUpdateJob(t *testing.T) {
	ts := newTestServer(t)
	_, err := ts.Client.UpdateJob(context.Background(), &api.UpdateJobRequest{})
	assert.Error(t, err)
	assert.Equal(t, codes.Unimplemented, grpc.Code(err))
}

func TestDeleteJob(t *testing.T) {
	ts := newTestServer(t)
	_, err := ts.Client.DeleteJob(context.Background(), &api.DeleteJobRequest{})
	assert.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, grpc.Code(err))

	job := createJob(ts, "name", "image", 1)
	r, err := ts.Client.DeleteJob(context.Background(), &api.DeleteJobRequest{JobID: job.ID})
	assert.NoError(t, err)
	assert.NotNil(t, r)
}

func TestListJobs(t *testing.T) {
	ts := newTestServer(t)
	r, err := ts.Client.ListJobs(context.Background(), &api.ListJobsRequest{})
	assert.NoError(t, err)
	assert.Equal(t, 0, len(r.Jobs))

	_ = createJob(ts, "name1", "image", 1)
	r, err = ts.Client.ListJobs(context.Background(), &api.ListJobsRequest{})
	assert.NoError(t, err)
	assert.Equal(t, 1, len(r.Jobs))

	_ = createJob(ts, "name2", "image", 1)
	_ = createJob(ts, "name3", "image", 1)
	r, err = ts.Client.ListJobs(context.Background(), &api.ListJobsRequest{})
	assert.NoError(t, err)
	assert.Equal(t, 3, len(r.Jobs))
}
