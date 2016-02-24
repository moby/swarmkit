package raft

import "sync"

// Cluster represents a set of active
// raft members
type Cluster struct {
	lock  sync.RWMutex
	peers map[uint64]*Peer
}

// Peer represents a raft cluster peer
type Peer struct {
	*NodeInfo

	Client *Raft
}

// NewCluster creates a new cluster neighbors
// list for a raft member
func NewCluster() *Cluster {
	return &Cluster{
		peers: make(map[uint64]*Peer),
	}
}

// Peers returns the list of peers in the cluster
func (c *Cluster) Peers() map[uint64]*Peer {
	peers := make(map[uint64]*Peer)
	c.lock.RLock()
	for k, v := range c.peers {
		peers[k] = v
	}
	c.lock.RUnlock()
	return peers
}

// AddPeer adds a node to our neighbors
func (c *Cluster) AddPeer(peer *Peer) {
	c.lock.Lock()
	c.peers[peer.ID] = peer
	c.lock.Unlock()
}

// RemovePeer removes a node from our neighbors
func (c *Cluster) RemovePeer(id uint64) {
	c.lock.Lock()
	delete(c.peers, id)
	c.lock.Unlock()
}
