package scheduler

import (
	"math"
	"math/rand"
	"strconv"
	"testing"

	"github.com/docker/swarm-v2/api"
	"github.com/stretchr/testify/assert"
)

func TestFindMin(t *testing.T) {
	var nh nodeHeap

	for reps := 0; reps < 10; reps++ {
		// Create a bunch of nodes with random numbers of tasks
		numNodes := 10000
		nh.alloc(numNodes)

		for i := 0; i != numNodes; i++ {
			n := &api.Node{
				ID: "id" + strconv.Itoa(i),
				Spec: api.NodeSpec{
					Annotations: api.Annotations{
						Labels: make(map[string]string),
					},
				},
			}

			// Delete some older nodes at random to make this an
			if i > 100 && rand.Intn(10) == 0 {
				nh.remove("id" + strconv.Itoa(i-100))
			}

			// Give every hundredth node a special label
			if i%100 == 0 {
				n.Spec.Annotations.Labels["special"] = "true"
			}
			tasks := make(map[string]*api.Task)
			for i := rand.Intn(25); i > 0; i-- {
				tasks[strconv.Itoa(i)] = &api.Task{ID: strconv.Itoa(i)}
			}
			nh.addOrUpdateNode(NodeInfo{Node: n, Tasks: tasks})
		}

		isSpecial := func(n *NodeInfo) bool {
			return n.Spec.Annotations.Labels["special"] == "true"
		}

		bestNode, numTasks := nh.findMin(isSpecial, false)
		assert.NotNil(t, bestNode)

		// Verify with manual search
		var manualBestNode *api.Node
		manualBestTasks := uint64(math.MaxUint64)
		for i := 0; i < nh.Len(); i++ {
			if !isSpecial(&nh.heap[i]) {
				continue
			}
			if uint64(len(nh.heap[i].Tasks)) < manualBestTasks {
				manualBestNode = nh.heap[i].Node
				manualBestTasks = uint64(len(nh.heap[i].Tasks))
			} else if uint64(len(nh.heap[i].Tasks)) == manualBestTasks && nh.heap[i].Node == bestNode {
				// prefer the node that findMin chose when
				// there are multiple best choices
				manualBestNode = nh.heap[i].Node
			}
		}

		assert.Equal(t, bestNode, manualBestNode)
		assert.Equal(t, numTasks, int(manualBestTasks))
	}
}
