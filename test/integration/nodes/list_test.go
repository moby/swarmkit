package nodes

import (
	"testing"

	"github.com/docker/swarm-v2/test/integration"
	"github.com/stretchr/testify/assert"
)

func TestListNodes(t *testing.T) {
	integration.StartManagers(1)
	defer integration.StopManagers()

	integration.StartAgents(2)
	defer integration.StopAgents()

	output, err := integration.SwarmCtl("node", "ls", "-q")
	assert.NoError(t, err)
	assert.EqualValues(t, 2, len(output.Lines()))

}
