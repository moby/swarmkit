package state

import (
	"sync"

	"github.com/docker/swarm-v2/api"
)

// Cluster represents a raft cluster
type Cluster interface {
	// ID returns the cluster ID
	ID() uint64
	// Members returns a map of members identified by their raft ID
	Members() map[uint64]*Member
	// AddMember adds a member to the cluster memberlist
	AddMember(*Member) error
	// RemoveMember removes a member from the memberlist and add
	// it to the remove set
	RemoveMember(uint64) error
	// Member retrieves a particular member based on ID, or nil if the
	// member does not exist in the cluster
	GetMember(id uint64) *Member
	// IsIDRemoved checks whether the given ID has been removed from this
	// cluster at some point in the past
	IsIDRemoved(id uint64) bool
}

// cluster represents a set of active
// raft members
type cluster struct {
	id uint64

	mu      sync.RWMutex
	members map[uint64]*Member

	// removed contains the list of removed members,
	// those ids cannot be reused
	removed map[uint64]bool
}

// Member represents a raft cluster member
type Member struct {
	*api.RaftNode

	Client *Raft
}

// NewCluster creates a new cluster neighbors
// list for a raft member
func NewCluster() Cluster {
	// TODO generate cluster ID

	return &cluster{
		members: make(map[uint64]*Member),
		removed: make(map[uint64]bool),
	}
}

func (c *cluster) ID() uint64 {
	return c.id
}

// Members returns the list of raft members in the cluster
func (c *cluster) Members() map[uint64]*Member {
	members := make(map[uint64]*Member)
	c.mu.RLock()
	for k, v := range c.members {
		members[k] = v
	}
	c.mu.RUnlock()
	return members
}

// GetMember returns informations on a given member
func (c *cluster) GetMember(id uint64) *Member {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.members[id]
}

// AddMember adds a node to the cluster memberlist
func (c *cluster) AddMember(member *Member) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.members[member.ID] = member
	return nil
}

// RemoveMember removes a node from the cluster memberlist
func (c *cluster) RemoveMember(id uint64) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.members[id].Client.Conn.Close()
	c.removed[id] = true
	delete(c.members, id)
	return nil
}

// IsIDRemoved checks if a member is in the remove set
func (c *cluster) IsIDRemoved(id uint64) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.removed[id]
}
