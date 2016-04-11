package scheduler

import (
	"container/heap"
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
				Spec: &api.NodeSpec{
					Meta: api.Meta{
						Labels: make(map[string]string),
					},
				},
			}

			// Give every hundredth node a special label
			if i%100 == 0 {
				n.Spec.Meta.Labels["special"] = "true"
			}
			nh.heap = append(nh.heap, NodeInfo{Node: n, NumTasks: int(rand.Int())})
			nh.index[n.ID] = i
		}

		heap.Init(&nh)

		isSpecial := func(n NodeInfo) bool {
			return n.Spec.Meta.Labels["special"] == "true"
		}

		bestNode, numTasks := nh.findMin(isSpecial, false)
		assert.NotNil(t, bestNode)

		// Verify with manual search
		var manualBestNode *api.Node
		manualBestTasks := uint64(math.MaxUint64)
		for i := 0; i < nh.Len(); i++ {
			if !isSpecial(nh.heap[i]) {
				continue
			}
			if uint64(nh.heap[i].NumTasks) < manualBestTasks {
				manualBestNode = nh.heap[i].Node
				manualBestTasks = uint64(nh.heap[i].NumTasks)
			} else if uint64(nh.heap[i].NumTasks) == manualBestTasks && nh.heap[i].Node == bestNode {
				// prefer the node that findMin chose when
				// there are multiple best choices
				manualBestNode = nh.heap[i].Node
			}
		}

		assert.Equal(t, bestNode, manualBestNode)
		assert.Equal(t, numTasks, int(manualBestTasks))
	}
}
