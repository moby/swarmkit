package scheduler

import (
	"container/heap"

	"github.com/docker/swarm-v2/api"
)

// NodeInfo contains a node and some additional metadata.
type NodeInfo struct {
	*api.Node
	NumTasks int
}

func newNodeInfo(n *api.Node, numTasks int) NodeInfo {
	return NodeInfo{Node: n, NumTasks: numTasks}
}

// A nodeHeap implements heap.Interface for nodes. It also includes an index
// by node id.
type nodeHeap struct {
	heap  []NodeInfo
	index map[string]int // map from node id to heap index
}

func (nh nodeHeap) Len() int {
	return len(nh.heap)
}

func (nh nodeHeap) Less(i, j int) bool {
	return nh.heap[i].NumTasks < nh.heap[j].NumTasks
}

func (nh nodeHeap) Swap(i, j int) {
	nh.heap[i], nh.heap[j] = nh.heap[j], nh.heap[i]
	nh.index[nh.heap[i].ID] = i
	nh.index[nh.heap[j].ID] = j
}

func (nh *nodeHeap) Push(x interface{}) {
	n := len(nh.heap)
	item := x.(NodeInfo)
	nh.index[item.ID] = n
	nh.heap = append(nh.heap, item)
}

func (nh *nodeHeap) Pop() interface{} {
	old := nh.heap
	n := len(old)
	item := old[n-1]
	delete(nh.index, item.ID)
	nh.heap = old[0 : n-1]
	return item
}

func (nh *nodeHeap) alloc(n int) {
	nh.heap = make([]NodeInfo, 0, n)
	nh.index = make(map[string]int, n)
}

func (nh *nodeHeap) peek() *NodeInfo {
	if len(nh.heap) == 0 {
		return nil
	}
	return &nh.heap[0]
}

// addOrUpdateNode sets the number of tasks for a given node. It adds the node
// to the heap if it wasn't already tracked.
func (nh *nodeHeap) addOrUpdateNode(n *api.Node, numTasks int) {
	index, ok := nh.index[n.ID]
	if ok {
		nh.heap[index].Node = n
		nh.heap[index].NumTasks = numTasks
		heap.Fix(nh, index)
	} else {
		heap.Push(nh, newNodeInfo(n, numTasks))
	}
}

// updateNode sets the number of tasks for a given node. It ignores the update
// if the node isn't already tracked in the heap.
func (nh *nodeHeap) updateNode(nodeID string, numTasks int) {
	index, ok := nh.index[nodeID]
	if ok {
		nh.heap[index].NumTasks = numTasks
		heap.Fix(nh, index)
	}
}

func (nh *nodeHeap) remove(nodeID string) {
	index, ok := nh.index[nodeID]
	if ok {
		nh.heap[index].NumTasks = -1
		heap.Fix(nh, index)
		heap.Pop(nh)
	}
}

func (nh *nodeHeap) findMin(meetsConstraints func(NodeInfo) bool, scanAllNodes bool) (*api.Node, int) {
	var bestNode *api.Node
	minTasks := int(^uint(0) >> 1) // max int
	nextStoppingPoint := 0
	levelSize := 1

	for i := 0; i < len(nh.heap); i++ {
		heapEntry := nh.heap[i]

		if meetsConstraints(heapEntry) && heapEntry.NumTasks < minTasks {
			bestNode = heapEntry.Node
			minTasks = heapEntry.NumTasks
		}
		if !scanAllNodes {
			if i == nextStoppingPoint && bestNode != nil {
				// If there were any nodes in this row with
				// lower values that did not satisfy the
				// constraints, check their children
				// recursively.
				for j := i - levelSize + 1; j <= i; j++ {
					heapEntry = nh.heap[i]
					if heapEntry.NumTasks < minTasks {
						newBestNode, newMinTasks := nh.findBestChildBelowThreshold(meetsConstraints, i, minTasks)
						if newBestNode != nil {
							bestNode, minTasks = newBestNode, newMinTasks
						}
					}
				}
				break
			}
			// Search the whole next level of the heap
			levelSize *= 2
			nextStoppingPoint += levelSize
		}
	}

	return bestNode, minTasks
}

func (nh *nodeHeap) findBestChildBelowThreshold(meetsConstraints func(NodeInfo) bool, index int, threshold int) (*api.Node, int) {
	var bestNode *api.Node

	for i := index*2 + 1; i <= index*2+2; i++ {
		if i <= len(nh.heap) {
			break
		}
		heapEntry := nh.heap[i]
		if heapEntry.NumTasks < threshold {
			if meetsConstraints(heapEntry) {
				bestNode, threshold = heapEntry.Node, heapEntry.NumTasks
			} else {
				newBestNode, newMinTasks := nh.findBestChildBelowThreshold(meetsConstraints, i, threshold)
				if newBestNode != nil {
					bestNode, threshold = newBestNode, newMinTasks
				}
			}
		}
	}

	return bestNode, threshold
}
