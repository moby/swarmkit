package nodes

import (
	"fmt"
	"testing"

	"github.com/docker/swarm-v2/test/integration"
	"github.com/stretchr/testify/assert"
)

func TestListJobs(t *testing.T) {
	integration.StartManagers(1)
	defer integration.StopManagers()

	output, err := integration.SwarmCtl("job", "ls", "-q")
	assert.NoError(t, err)
	fmt.Printf("%q\n", output)
	assert.EqualValues(t, 0, len(output.Lines()))

	_, err = integration.SwarmCtl("job", "create", "--name=job0", "--image=image")
	assert.NoError(t, err)
	output, err = integration.SwarmCtl("job", "ls", "-q")
	assert.NoError(t, err)
	assert.EqualValues(t, 1, len(output.Lines()))

	_, err = integration.SwarmCtl("job", "create", "--name=job1", "--image=image")
	assert.NoError(t, err)

	output, err = integration.SwarmCtl("job", "ls", "-q")
	assert.NoError(t, err)
	assert.EqualValues(t, 2, len(output.Lines()))

}
