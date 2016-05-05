package raft

import (
	"sync"

	"google.golang.org/grpc"

	"github.com/docker/swarm-v2/api"
)

// cluster represents a set of active
// raft members
type cluster struct {
	id uint64

	mu      sync.RWMutex
	members map[uint64]*member

	// removed contains the list of removed members,
	// those ids cannot be reused
	removed map[uint64]bool
}

// member represents a raft cluster member
type member struct {
	*api.Member

	Client *Raft
}

// newCluster creates a new cluster neighbors
// list for a raft member
func newCluster() *cluster {
	// TODO generate cluster ID

	return &cluster{
		members: make(map[uint64]*member),
		removed: make(map[uint64]bool),
	}
}

// listMembers returns the list of raft members in the cluster.
func (c *cluster) listMembers() map[uint64]*member {
	members := make(map[uint64]*member)
	c.mu.RLock()
	for k, v := range c.members {
		members[k] = v
	}
	c.mu.RUnlock()
	return members
}

// listRemoved returns the list of raft members removed from the cluster.
func (c *cluster) listRemoved() []uint64 {
	c.mu.RLock()
	removed := make([]uint64, 0, len(c.removed))
	for k := range c.removed {
		removed = append(removed, k)
	}
	c.mu.RUnlock()
	return removed
}

// getMember returns informations on a given member.
func (c *cluster) getMember(id uint64) *member {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.members[id]
}

// addMember adds a node to the cluster memberlist.
func (c *cluster) addMember(member *member) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.members[member.RaftID] = member
}

// removeMember removes a node from the cluster memberlist.
func (c *cluster) removeMember(id uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	conn := c.members[id].Client.Conn
	if conn != nil {
		_ = conn.Close()
	}
	c.removed[id] = true
	delete(c.members, id)
}

// isIDRemoved checks if a member is in the remove set.
func (c *cluster) isIDRemoved(id uint64) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.removed[id]
}

// clear resets the list of active members and removed members.
func (c *cluster) clear() {
	c.mu.Lock()
	c.members = make(map[uint64]*member)
	c.removed = make(map[uint64]bool)
	c.mu.Unlock()
}

// CanRemoveMember checks if removing a member would not result in a loss
// of quorum, this check is needed before submitting a configuration change
// that might block or harm the cluster on member recovery
func (c *cluster) CanRemoveMember(from uint64, id uint64) bool {
	members := c.listMembers()

	nmembers := 0
	nreachable := 0

	for _, member := range members {
		// Skip the node that is going to be deleted
		if member.RaftID == id {
			continue
		}

		// Local node from where the remove is issued
		if member.RaftID == from {
			nmembers++
			nreachable++
			continue
		}

		connState, err := member.Client.Conn.State()
		if err == nil && connState == grpc.Ready {
			nreachable++
		}

		nmembers++
	}

	// Special case of 2 managers
	if nreachable == 1 && len(members) <= 2 {
		return false
	}

	nquorum := nmembers/2 + 1
	if nreachable < nquorum {
		return false
	}

	return true
}
