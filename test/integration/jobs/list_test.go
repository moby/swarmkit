package nodes

import (
	"testing"

	"github.com/docker/swarm-v2/test/integration"
	"github.com/stretchr/testify/assert"
)

func TestListJobs(t *testing.T) {
	test := integration.Test{}
	test.StartManagers(1)
	defer test.StopManagers()

	output, code, err := test.SwarmCtl("job", "ls", "-q")
	assert.NoError(t, err)
	assert.Equal(t, 0, code)
	assert.EqualValues(t, 0, len(output.Lines()))

	_, code, err = test.SwarmCtl("job", "create", "--name=job0", "--image=image")
	assert.NoError(t, err)
	assert.Equal(t, 0, code)
	output, code, err = test.SwarmCtl("job", "ls", "-q")
	assert.NoError(t, err)
	assert.Equal(t, 0, code)
	assert.EqualValues(t, 1, len(output.Lines()))

	_, code, err = test.SwarmCtl("job", "create", "--name=job1", "--image=image")
	assert.NoError(t, err)
	assert.Equal(t, 0, code)

	output, code, err = test.SwarmCtl("job", "ls", "-q")
	assert.NoError(t, err)
	assert.Equal(t, 0, code)
	assert.EqualValues(t, 2, len(output.Lines()))

}
