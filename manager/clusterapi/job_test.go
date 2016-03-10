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
		Template: &api.TaskSpec{
			Runtime: &api.TaskSpec_Container{
				Container: &api.ContainerSpec{
					Image: &api.ImageSpec{
						Reference: image,
					},
				},
			},
		},
		Orchestration: &api.JobSpec_Service{
			Service: &api.JobSpec_ServiceJob{
				Instances: instances,
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
	type BadMeta struct {
		m *api.Meta
		c codes.Code
	}

	for _, bad := range []BadMeta{
		{
			m: nil,
			c: codes.InvalidArgument,
		},
		{
			m: &api.Meta{},
			c: codes.InvalidArgument,
		},
	} {
		err := validateJobSpecMeta(bad.m)
		assert.Error(t, err)
		assert.Equal(t, bad.c, grpc.Code(err))
	}

	for _, good := range []*api.Meta{
		{Name: "name"},
	} {
		err := validateJobSpecMeta(good)
		assert.NoError(t, err)
	}
}

func TestValidateJobSpecTemplate(t *testing.T) {
	type badSource struct {
		s *api.JobSpec
		c codes.Code
	}

	for _, bad := range []badSource{
		{
			s: &api.JobSpec{Template: nil},
			c: codes.InvalidArgument,
		},
		{
			s: &api.JobSpec{
				Template: &api.TaskSpec{
					Runtime: nil,
				},
			},
			c: codes.InvalidArgument,
		},
		// NOTE(stevvooe): can't actually test this case because we don't have
		// another runtime defined.
		// {
		// 	s: &api.JobSpec{
		// 		Template: &api.TaskSpec{
		// 			Runtime:
		// 		},
		// 	},
		// 	c: codes.Unimplemented,
		// },
		{
			s: createSpec("", "", 0),
			c: codes.InvalidArgument,
		},
	} {
		err := validateJobSpecTemplate(bad.s)
		assert.Error(t, err)
		assert.Equal(t, bad.c, grpc.Code(err))
	}

	for _, good := range []*api.JobSpec{
		createSpec("", "image", 0),
	} {
		err := validateJobSpecTemplate(good)
		assert.NoError(t, err)
	}
}

func TestValidateJobSpecOrchestration(t *testing.T) {
	type BadJobSpecOrchestration struct {
		s *api.JobSpec
		c codes.Code
	}

	for _, bad := range []BadJobSpecOrchestration{
		{
			s: &api.JobSpec{Orchestration: nil},
			c: codes.InvalidArgument,
		},
		{
			s: &api.JobSpec{Orchestration: &api.JobSpec_Service{}},
			c: codes.InvalidArgument,
		},
		{
			s: &api.JobSpec{Orchestration: &api.JobSpec_Batch{}},
			c: codes.Unimplemented,
		},
	} {
		err := validateJobSpecOrchestration(bad.s)
		assert.Error(t, err)
		assert.Equal(t, bad.c, grpc.Code(err))
	}

	for _, good := range []*api.JobSpec{
		createSpec("", "", 1),
	} {
		err := validateJobSpecOrchestration(good)
		assert.NoError(t, err)
	}
}

func TestValidateJobSpec(t *testing.T) {
	type BadJobSpec struct {
		spec *api.JobSpec
		c    codes.Code
	}

	for _, bad := range []BadJobSpec{
		{
			spec: nil,
			c:    codes.InvalidArgument,
		},
		{
			spec: &api.JobSpec{Meta: &api.Meta{Name: "name"}},
			c:    codes.InvalidArgument,
		},
		{
			spec: createSpec("", "", 1),
			c:    codes.InvalidArgument,
		},
		{
			spec: createSpec("name", "", 1),
			c:    codes.InvalidArgument,
		},
		{
			spec: createSpec("", "image", 1),
			c:    codes.InvalidArgument,
		},
	} {
		err := validateJobSpec(bad.spec)
		assert.Error(t, err)
		assert.Equal(t, bad.c, grpc.Code(err))
	}

	for _, good := range []*api.JobSpec{
		createSpec("name", "image", 1),
	} {
		err := validateJobSpec(good)
		assert.NoError(t, err)
	}
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

func TestRemoveJob(t *testing.T) {
	ts := newTestServer(t)
	_, err := ts.Client.RemoveJob(context.Background(), &api.RemoveJobRequest{})
	assert.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, grpc.Code(err))

	job := createJob(ts, "name", "image", 1)
	r, err := ts.Client.RemoveJob(context.Background(), &api.RemoveJobRequest{JobID: job.ID})
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
