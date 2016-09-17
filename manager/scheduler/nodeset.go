package scheduler

import (
	"errors"
	"sort"

	"github.com/aaronlehmann/quickselect"
)

var errNodeNotFound = errors.New("node not found in scheduler dataset")

type nodeSet struct {
	nodes map[string]NodeInfo // map from node id to node info
}

func (ns *nodeSet) alloc(n int) {
	ns.nodes = make(map[string]NodeInfo, n)
}

// nodeInfo returns the NodeInfo struct for a given node identified by its ID.
func (ns *nodeSet) nodeInfo(nodeID string) (NodeInfo, error) {
	node, ok := ns.nodes[nodeID]
	if ok {
		return node, nil
	}
	return NodeInfo{}, errNodeNotFound
}

// addOrUpdateNode sets the number of tasks for a given node. It adds the node
// to the set if it wasn't already tracked.
func (ns *nodeSet) addOrUpdateNode(n NodeInfo) {
	ns.nodes[n.ID] = n
}

// updateNode sets the number of tasks for a given node. It ignores the update
// if the node isn't already tracked in the set.
func (ns *nodeSet) updateNode(n NodeInfo) {
	_, ok := ns.nodes[n.ID]
	if ok {
		ns.nodes[n.ID] = n
	}
}

func (ns *nodeSet) remove(nodeID string) {
	delete(ns.nodes, nodeID)
}

type nodeSorter struct {
	nodes    []NodeInfo
	lessFunc func(*NodeInfo, *NodeInfo) bool
}

func (s nodeSorter) Len() int {
	return len(s.nodes)
}

func (s nodeSorter) Swap(i, j int) {
	s.nodes[i], s.nodes[j] = s.nodes[j], s.nodes[i]
}

func (s nodeSorter) Less(i, j int) bool {
	return s.lessFunc(&s.nodes[i], &s.nodes[j])
}

// findBestNodes returns n nodes (or < n if fewer nodes are available) that
// rank best (lowest) according to the sorting function.
func (ns *nodeSet) findBestNodes(n int, meetsConstraints func(*NodeInfo) bool, nodeLess func(*NodeInfo, *NodeInfo) bool) []NodeInfo {
	var nodes []NodeInfo
	// TODO(aaronl): Is is possible to avoid checking constraints on every
	// node? Perhaps we should quickselect n*2 nodes that weren't
	// prescreened, and repeat the quickselect if there weren't enough
	// nodes meeting the constraints.
	for _, node := range ns.nodes {
		if meetsConstraints(&node) {
			nodes = append(nodes, node)
		}
	}

	if len(nodes) > n {
		quickselect.QuickSelect(nodeSorter{nodes: nodes, lessFunc: nodeLess}, n)
		nodes = nodes[:n]
	}

	sort.Sort(nodeSorter{nodes: nodes, lessFunc: nodeLess})
	return nodes
}
