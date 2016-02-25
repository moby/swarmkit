package raft

import (
	"errors"
	"fmt"
	"log"
	"math"
	"net"
	"os"
	"sync"
	"time"

	"google.golang.org/grpc"

	"golang.org/x/net/context"

	"github.com/coreos/etcd/raft"
	"github.com/coreos/etcd/raft/raftpb"
	"github.com/gogo/protobuf/proto"
)

var (
	defaultLogger = &raft.DefaultLogger{Logger: log.New(os.Stderr, "raft", log.LstdFlags)}

	// ErrConnectionRefused is thrown when a connection is refused to a node member in the raft
	ErrConnectionRefused = errors.New("connection refused to the node")
	// ErrConfChangeRefused is thrown when there is an issue with the configuration change
	ErrConfChangeRefused = errors.New("propose configuration change refused")
	// ErrApplyNotSpecified is thrown during the creation of a raft node when no apply method was provided
	ErrApplyNotSpecified = errors.New("apply method was not specified")
)

// ApplyCommand function can be used and triggered
// every time there is an append entry event
type ApplyCommand func(interface{})

// Node represents the Raft Node useful
// configuration.
type Node struct {
	raft.Node

	Client   *Raft
	Cluster  *Cluster
	Server   *grpc.Server
	Listener net.Listener
	Ctx      context.Context

	ID      uint64
	Address string
	Port    int
	Error   error

	storeLock sync.RWMutex
	PStore    map[string]string
	Store     *raft.MemoryStorage
	Cfg       *raft.Config

	ticker   *time.Ticker
	stopChan chan struct{}

	// ApplyCommand is called when a log entry
	// is committed to the logs, behind can
	// lie any kind of logic processing the
	// message
	apply ApplyCommand
}

// NewNode generates a new Raft node based on an unique
// ID, an address and optionally: a handler and receive
// only channel to send event when an entry is committed
// to the logs
func NewNode(id uint64, addr string, cfg *raft.Config, apply ApplyCommand) (*Node, error) {
	if cfg == nil {
		cfg = DefaultNodeConfig()
	}

	store := raft.NewMemoryStorage()
	peers := []raft.Peer{{ID: id}}

	n := &Node{
		ID:      id,
		Ctx:     context.TODO(),
		Cluster: NewCluster(),
		Store:   store,
		Address: addr,
		Cfg: &raft.Config{
			ID:              id,
			ElectionTick:    cfg.ElectionTick,
			HeartbeatTick:   cfg.HeartbeatTick,
			Storage:         store,
			MaxSizePerMsg:   cfg.MaxSizePerMsg,
			MaxInflightMsgs: cfg.MaxInflightMsgs,
			Logger:          cfg.Logger,
		},
		PStore:   make(map[string]string),
		ticker:   time.NewTicker(time.Second),
		stopChan: make(chan struct{}),
		apply:    apply,
	}

	n.Cluster.AddPeer(
		&Peer{
			NodeInfo: &NodeInfo{
				ID:   id,
				Addr: addr,
			},
		},
	)

	n.Node = raft.StartNode(n.Cfg, peers)
	return n, nil
}

// DefaultNodeConfig returns the default config for a
// raft node that can be modified and customized
func DefaultNodeConfig() *raft.Config {
	return &raft.Config{
		HeartbeatTick:   1,
		ElectionTick:    3,
		MaxSizePerMsg:   math.MaxUint16,
		MaxInflightMsgs: 256,
		Logger:          defaultLogger,
	}
}

// Start is the main loop for a Raft node, it
// goes along the state machine, acting on the
// messages received from other Raft nodes in
// the cluster
func (n *Node) Start() {
	for {
		select {
		case <-n.ticker.C:
			n.Tick()

		case rd := <-n.Ready():
			n.saveToStorage(rd.HardState, rd.Entries, rd.Snapshot)
			n.send(rd.Messages)
			if !raft.IsEmptySnap(rd.Snapshot) {
				n.processSnapshot(rd.Snapshot)
			}
			for _, entry := range rd.CommittedEntries {
				n.process(entry)
				if entry.Type == raftpb.EntryConfChange {
					var cc raftpb.ConfChange
					err := cc.Unmarshal(entry.Data)
					if err != nil {
						log.Fatal("raft: Can't unmarshal configuration change")
					}
					switch cc.Type {
					case raftpb.ConfChangeAddNode:
						n.applyAddNode(cc)
					case raftpb.ConfChangeRemoveNode:
						n.applyRemoveNode(cc)
					}
					n.ApplyConfChange(cc)
				}
			}
			n.Advance()

		case <-n.stopChan:
			n.Stop()
			n.Node = nil
			close(n.stopChan)
			return
		}
	}
}

// Shutdown stops the raft node processing loop.
// Calling Shutdown on an already stopped node
// will result in a deadlock
func (n *Node) Shutdown() {
	n.stopChan <- struct{}{}
}

// IsLeader checks if we are the leader or not
func (n *Node) IsLeader() bool {
	if n.Node.Status().Lead == n.ID {
		return true
	}
	return false
}

// Leader returns the id of the leader
func (n *Node) Leader() uint64 {
	return n.Node.Status().Lead
}

// JoinRaft asks to a member of the raft to propose
// a configuration change and add us as a member thus
// beginning the log replication process. This method
// is called from an aspiring member to an existing member
func (n *Node) JoinRaft(ctx context.Context, info *NodeInfo) (*JoinRaftResponse, error) {
	meta, err := proto.Marshal(info)
	if err != nil {
		log.Fatal("raft: Can't marshal node: ", info.ID)
	}

	confChange := raftpb.ConfChange{
		ID:      info.ID,
		Type:    raftpb.ConfChangeAddNode,
		NodeID:  info.ID,
		Context: meta,
	}

	err = n.ProposeConfChange(n.Ctx, confChange)
	if err != nil {
		return &JoinRaftResponse{
			Success: false,
			Error:   ErrConfChangeRefused.Error(),
		}, nil
	}

	var nodes []*NodeInfo
	for _, node := range n.Cluster.Peers() {
		nodes = append(nodes, &NodeInfo{
			ID:   node.ID,
			Addr: node.Addr,
		})
	}

	// TODO (abronan): instead of sending back
	// the list of nodes and let the new member
	// add them itself to its local list: grpc
	// call add from the node sending the conf
	// change
	return &JoinRaftResponse{
		Success: true,
		Nodes:   nodes,
		Error:   "",
	}, nil
}

// LeaveRaft asks to a member of the raft to remove
// us from the raft cluster. This method is called
// from a member who is willing to leave its raft
// membership to an active member of the raft
func (n *Node) LeaveRaft(ctx context.Context, info *NodeInfo) (*LeaveRaftResponse, error) {
	confChange := raftpb.ConfChange{
		ID:      info.ID,
		Type:    raftpb.ConfChangeRemoveNode,
		NodeID:  info.ID,
		Context: []byte(""),
	}

	err := n.ProposeConfChange(n.Ctx, confChange)
	if err != nil {
		return &LeaveRaftResponse{
			Success: false,
			Error:   ErrConfChangeRefused.Error(),
		}, nil
	}

	return &LeaveRaftResponse{
		Success: true,
		Error:   "",
	}, nil
}

// Send calls 'Step' which advances the raft state
// machine with the received message
func (n *Node) Send(ctx context.Context, msg *raftpb.Message) (*SendResponse, error) {
	err := n.Step(n.Ctx, *msg)
	if err != nil {
		return &SendResponse{Error: err.Error()}, nil
	}

	return &SendResponse{Error: ""}, nil
}

// RemoveNode removes a node from the raft cluster
func (n *Node) RemoveNode(node *Peer) error {
	confChange := raftpb.ConfChange{
		ID:      node.ID,
		Type:    raftpb.ConfChangeRemoveNode,
		NodeID:  node.ID,
		Context: []byte(""),
	}

	err := n.ProposeConfChange(n.Ctx, confChange)
	if err != nil {
		return err
	}
	return nil
}

// RegisterNode registers a new node on the cluster
func (n *Node) RegisterNode(node *NodeInfo) error {
	var (
		client *Raft
		err    error
	)

	for i := 1; i <= MaxRetryTime; i++ {
		client, err = GetRaftClient(node.Addr, 2*time.Second)
		if err != nil {
			if i == MaxRetryTime {
				return ErrConnectionRefused
			}
		}
	}

	n.Cluster.AddPeer(&Peer{NodeInfo: node, Client: client})
	return nil
}

// RegisterNodes registers a set of nodes in the cluster
func (n *Node) RegisterNodes(nodes []*NodeInfo) (err error) {
	for _, node := range nodes {
		err = n.RegisterNode(node)
		if err != nil {
			return err
		}
	}

	return nil
}

// UnregisterNode unregisters a node that has died or
// has gracefully left the raft subsystem
func (n *Node) UnregisterNode(id uint64) {
	// Do not unregister yourself
	if n.ID == id {
		return
	}

	n.Cluster.Peers()[id].Client.Conn.Close()
	n.Cluster.RemovePeer(id)
}

// Get returns a value from the PStore
func (n *Node) Get(key string) string {
	n.storeLock.RLock()
	defer n.storeLock.RUnlock()
	return n.PStore[key]
}

// Put puts a value in the raft store
func (n *Node) Put(key string, value string) {
	n.storeLock.Lock()
	defer n.storeLock.Unlock()
	n.PStore[key] = value
}

// StoreLength returns the length of the store
func (n *Node) StoreLength() int {
	n.storeLock.Lock()
	defer n.storeLock.Unlock()
	return len(n.PStore)
}

// applyAddNode is called when we receive a ConfChange
// from a member in the raft cluster, this adds a new
// node to the existing raft cluster
func (n *Node) applyAddNode(conf raftpb.ConfChange) error {
	peer := &NodeInfo{}
	err := proto.Unmarshal(conf.Context, peer)
	if err != nil {
		return err
	}
	if n.ID != peer.ID {
		n.RegisterNode(peer)
	}
	return nil
}

// applyRemoveNode is called when we receive a ConfChange
// from a member in the raft cluster, this removes a node
// from the existing raft cluster
func (n *Node) applyRemoveNode(conf raftpb.ConfChange) {
	// The leader steps down
	if n.ID == n.Leader() && n.ID == conf.NodeID {
		n.Stop()
		return
	}
	// If a follower and the leader steps
	// down, Campaign to be the leader
	if conf.NodeID == n.Leader() {
		n.Campaign(n.Ctx)
	}
	n.UnregisterNode(conf.NodeID)
}

// Saves a log entry to our Store
func (n *Node) saveToStorage(hardState raftpb.HardState, entries []raftpb.Entry, snapshot raftpb.Snapshot) {
	n.Store.Append(entries)

	if !raft.IsEmptyHardState(hardState) {
		n.Store.SetHardState(hardState)
	}

	if !raft.IsEmptySnap(snapshot) {
		n.Store.ApplySnapshot(snapshot)
	}
}

// Sends a series of messages to members in the raft
func (n *Node) send(messages []raftpb.Message) {
	peers := n.Cluster.Peers()

	for _, m := range messages {
		// Process locally
		if m.To == n.ID {
			n.Step(n.Ctx, m)
			continue
		}

		// If node is an active raft member send the message
		if peer, ok := peers[m.To]; ok {
			_, err := peer.Client.Send(n.Ctx, &m)
			if err != nil {
				n.ReportUnreachable(peer.ID)
			}
		}
	}
}

// Process a data entry and optionnally triggers an event
// or a function handler after the entry is processed
func (n *Node) process(entry raftpb.Entry) {
	if entry.Type == raftpb.EntryNormal && entry.Data != nil {
		// TODO (abronan, al, mrjana): replace KV pair
		// by internal store interface
		pair := &Pair{}
		err := proto.Unmarshal(entry.Data, pair)
		if err != nil {
			log.Fatal("raft: Can't decode key and value sent through raft")
		}

		// Apply the command
		if n.apply != nil {
			n.apply(entry.Data)
		}

		// Put the value into the store
		n.Put(pair.Key, string(pair.Value))
	}
}

// Process snapshot is not yet implemented but applies
// a snapshot to handle node failures and restart
func (n *Node) processSnapshot(snapshot raftpb.Snapshot) {
	// TODO
	panic(fmt.Sprintf("Applying snapshot on node %v is not implemented", n.ID))
}
