package clusterapi

import (
	"testing"

	"github.com/docker/swarm-v2/api"
	"github.com/stretchr/testify/assert"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
)

func TestCreateJob(t *testing.T) {
	ts := newTestServer(t)
	_, err := ts.Client.CreateJob(context.Background(), &api.CreateJobRequest{})
	assert.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, grpc.Code(err))

	r, err := ts.Client.CreateJob(context.Background(), &api.CreateJobRequest{Spec: &api.JobSpec{}})
	assert.NoError(t, err)
	assert.NotEmpty(t, r.Job.ID)
}

func TestGetJob(t *testing.T) {
	ts := newTestServer(t)
	_, err := ts.Client.GetJob(context.Background(), &api.GetJobRequest{})
	assert.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, grpc.Code(err))

	rc, err := ts.Client.CreateJob(context.Background(), &api.CreateJobRequest{Spec: &api.JobSpec{}})
	assert.NoError(t, err)
	rg, err := ts.Client.GetJob(context.Background(), &api.GetJobRequest{JobID: rc.Job.ID})
	assert.NoError(t, err)
	assert.Equal(t, rc.Job, rg.Job)
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

	rc, err := ts.Client.CreateJob(context.Background(), &api.CreateJobRequest{Spec: &api.JobSpec{}})
	assert.NoError(t, err)
	rd, err := ts.Client.DeleteJob(context.Background(), &api.DeleteJobRequest{JobID: rc.Job.ID})
	assert.NoError(t, err)
	assert.NotNil(t, rd)
}

func TestListJobs(t *testing.T) {
	ts := newTestServer(t)
	r, err := ts.Client.ListJobs(context.Background(), &api.ListJobsRequest{})
	assert.NoError(t, err)
	assert.Equal(t, 0, len(r.Jobs))

	_, err = ts.Client.CreateJob(context.Background(), &api.CreateJobRequest{Spec: &api.JobSpec{}})
	assert.NoError(t, err)
	r, err = ts.Client.ListJobs(context.Background(), &api.ListJobsRequest{})
	assert.NoError(t, err)
	assert.Equal(t, 1, len(r.Jobs))
}
