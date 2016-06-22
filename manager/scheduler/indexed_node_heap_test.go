package scheduler

import (
	"math"
	"math/rand"
	"strconv"
	"testing"

	"github.com/docker/swarmkit/api"
	"github.com/stretchr/testify/assert"
)

func findMin(t *testing.T, scanAllNodes bool) {
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

		bestNode, numTasks := nh.findMin(isSpecial, scanAllNodes)
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

// Test scan all nodes to find min
func TestFindMinScan(t *testing.T) {
	findMin(t, true)
}

// Test search in heap to find min
func TestFindMinHeap(t *testing.T) {
	findMin(t, false)
}

func TestRemove(t *testing.T) {
	var nh nodeHeap

	for reps := 0; reps < 10; reps++ {
		// Create a bunch of nodes with random numbers of tasks
		numNodes := 100
		nh.alloc(numNodes)

		for i := 0; i != numNodes; i++ {
			n := &api.Node{ID: strconv.Itoa(i)}
			tasks := make(map[string]*api.Task)
			for j := 0; j < rand.Intn(10); j++ {
				tasks[strconv.Itoa(j)] = &api.Task{ID: strconv.Itoa(j)}
			}
			nh.addOrUpdateNode(NodeInfo{Node: n, Tasks: tasks})
		}

		// delete 10 nodes in random
		for i := 0; i < 10; i++ {
			nodeID := nh.heap[rand.Intn(len(nh.heap))].ID

			for _, ok := nh.index[nodeID]; !ok; {
				// if this node is already deleted, re-try
				nodeID = nh.heap[rand.Intn(len(nh.heap))].ID
			}
			nh.remove(nodeID)
			for _, entry := range nh.heap {
				if entry.ID == nodeID {
					t.Fatalf("fail to remove node %v\n", nodeID)
				}
			}

			if _, ok := nh.index[nodeID]; ok {
				t.Fatalf("fail to remove node %v\n", nodeID)
			}
		}
	}
}
