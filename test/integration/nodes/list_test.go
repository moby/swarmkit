package nodes

import (
	"testing"

	"github.com/docker/swarm-v2/test/integration"
	"github.com/stretchr/testify/assert"
)

func TestListNodes(t *testing.T) {
	test := integration.Test{}
	test.StartManagers(1)
	test.StartAgents(2)
	defer test.Cleanup()

	output, err := test.SwarmCtl("node", "ls", "-q")
	assert.NoError(t, err)
	assert.EqualValues(t, 2, len(output.Lines()))

}
