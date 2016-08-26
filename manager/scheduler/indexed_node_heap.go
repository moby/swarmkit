package scheduler

import (
	"container/heap"
	"errors"
	"sort"

	"github.com/docker/swarmkit/api"
)

var errNodeNotFound = errors.New("node not found in scheduler heap")

// A nodeHeap implements heap.Interface for nodes. It also includes an index
// by node id.
// TODO(aaronl): Change this to a flat list and rename. The heap property is
// no longer useful.
type nodeHeap struct {
	heap  []NodeInfo
	index map[string]int // map from node id to heap index
}

func (nh nodeHeap) Len() int {
	return len(nh.heap)
}

func (nh nodeHeap) Less(i, j int) bool {
	return len(nh.heap[i].Tasks) < len(nh.heap[j].Tasks)
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

// nodeInfo returns the NodeInfo struct for a given node identified by its ID.
func (nh *nodeHeap) nodeInfo(nodeID string) (NodeInfo, error) {
	index, ok := nh.index[nodeID]
	if ok {
		return nh.heap[index], nil
	}
	return NodeInfo{}, errNodeNotFound
}

// addOrUpdateNode sets the number of tasks for a given node. It adds the node
// to the heap if it wasn't already tracked.
func (nh *nodeHeap) addOrUpdateNode(n NodeInfo) {
	index, ok := nh.index[n.ID]
	if ok {
		nh.heap[index] = n
		heap.Fix(nh, index)
	} else {
		heap.Push(nh, n)
	}
}

// updateNode sets the number of tasks for a given node. It ignores the update
// if the node isn't already tracked in the heap.
func (nh *nodeHeap) updateNode(n NodeInfo) {
	index, ok := nh.index[n.ID]
	if ok {
		nh.heap[index] = n
		heap.Fix(nh, index)
	}
}

func (nh *nodeHeap) remove(nodeID string) {
	index, ok := nh.index[nodeID]
	if ok {
		heap.Remove(nh, index)
	}
}

func (nh *nodeHeap) findMin(meetsConstraints func(*NodeInfo) bool, scanAllNodes bool) (*api.Node, int) {
	if scanAllNodes {
		return nh.scanAllToFindMin(meetsConstraints)
	}
	return nh.searchHeapToFindMin(meetsConstraints)
}

type sorter struct {
	nodes    []*NodeInfo
	lessFunc func(*NodeInfo, *NodeInfo) bool
}

func (s sorter) Len() int {
	return len(s.nodes)
}

func (s sorter) Swap(i, j int) {
	s.nodes[i], s.nodes[j] = s.nodes[j], s.nodes[i]
}

func (s sorter) Less(i, j int) bool {
	return s.lessFunc(s.nodes[i], s.nodes[j])
}

// findBestNodes returns n nodes (or < n if fewer nodes are available) that
// rank best (lowest) according to the sorting function.
func (nh *nodeHeap) findBestNodes(n int, meetsConstraints func(*NodeInfo) bool, nodeLess func(*NodeInfo, *NodeInfo) bool) []*NodeInfo {
	var nodes []*NodeInfo
	for i := 0; i < len(nh.heap); i++ {
		if meetsConstraints(&nh.heap[i]) {
			nodes = append(nodes, &nh.heap[i])
		}
	}

	// TODO(aaronl): Use quickselect instead of sorting. Also, once we
	// switch to quickselect, avoid checking constraints on every node, and
	// instead do it lazily as part of the quickselect process (treat any
	// node that doesn't meet the constraints as greater than all nodes that
	// do, and cache the result of the constraint evaluation).
	sort.Sort(sorter{nodes: nodes, lessFunc: nodeLess})

	if len(nodes) < n {
		return nodes
	}

	return nodes[:n]
}

// Scan All nodes to find the best node which meets the constraints && has lightest workloads
func (nh *nodeHeap) scanAllToFindMin(meetsConstraints func(*NodeInfo) bool) (*api.Node, int) {
	var bestNode *api.Node
	minTasks := int(^uint(0) >> 1) // max int

	for i := 0; i < len(nh.heap); i++ {
		heapEntry := &nh.heap[i]
		if meetsConstraints(heapEntry) && len(heapEntry.Tasks) < minTasks {
			bestNode = heapEntry.Node
			minTasks = len(heapEntry.Tasks)
		}
	}

	return bestNode, minTasks
}

// Search in heap to find the best node which meets the constraints && has lightest workloads
func (nh *nodeHeap) searchHeapToFindMin(meetsConstraints func(*NodeInfo) bool) (*api.Node, int) {
	var bestNode *api.Node
	minTasks := int(^uint(0) >> 1) // max int

	if nh == nil || len(nh.heap) == 0 {
		return bestNode, minTasks
	}

	// push root to stack for search
	stack := []int{0}

	for len(stack) != 0 {
		// pop an element
		idx := stack[len(stack)-1]
		stack = stack[0 : len(stack)-1]

		heapEntry := &nh.heap[idx]

		if len(heapEntry.Tasks) >= minTasks {
			continue
		}

		if meetsConstraints(heapEntry) {
			// meet constraints, update results
			bestNode = heapEntry.Node
			minTasks = len(heapEntry.Tasks)
		} else {
			// otherwise, push 2 children to stack for further search
			if 2*idx+1 < len(nh.heap) {
				stack = append(stack, 2*idx+1)
			}
			if 2*idx+2 < len(nh.heap) {
				stack = append(stack, 2*idx+2)
			}
		}
	}
	return bestNode, minTasks
}
