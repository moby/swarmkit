package nodes

import (
	"fmt"
	"testing"

	"github.com/docker/swarm-v2/test/integration"
	"github.com/stretchr/testify/assert"
)

func TestListJobs(t *testing.T) {
	test := integration.Test{}
	test.StartManagers(1)
	defer test.StopManagers()

	output, err := test.SwarmCtl("job", "ls", "-q")
	assert.NoError(t, err)
	fmt.Printf("%q\n", output)
	assert.EqualValues(t, 0, len(output.Lines()))

	_, err = test.SwarmCtl("job", "create", "--name=job0", "--image=image")
	assert.NoError(t, err)
	output, err = test.SwarmCtl("job", "ls", "-q")
	assert.NoError(t, err)
	assert.EqualValues(t, 1, len(output.Lines()))

	_, err = test.SwarmCtl("job", "create", "--name=job1", "--image=image")
	assert.NoError(t, err)

	output, err = test.SwarmCtl("job", "ls", "-q")
	assert.NoError(t, err)
	assert.EqualValues(t, 2, len(output.Lines()))

}
