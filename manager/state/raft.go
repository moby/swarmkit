package state

import (
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"

	"google.golang.org/grpc"

	"golang.org/x/net/context"

	"github.com/Sirupsen/logrus"
	"github.com/coreos/etcd/pkg/idutil"
	"github.com/coreos/etcd/pkg/wait"
	"github.com/coreos/etcd/raft"
	"github.com/coreos/etcd/raft/raftpb"
	"github.com/coreos/etcd/snap"
	"github.com/coreos/etcd/wal"
	"github.com/coreos/etcd/wal/walpb"
	"github.com/docker/swarm-v2/api"
	"github.com/gogo/protobuf/proto"
)

const (
	maxRequestBytes       = 1.5 * 1024 * 1024
	defaultProposeTimeout = 10 * time.Second
)

var (
	defaultLogger = &raft.DefaultLogger{Logger: log.New(os.Stderr, "raft", log.LstdFlags)}

	// ErrConfChangeRefused is thrown when there is an issue with the configuration change
	ErrConfChangeRefused = errors.New("raft: propose configuration change refused")
	// ErrApplyNotSpecified is thrown during the creation of a raft node when no apply method was provided
	ErrApplyNotSpecified = errors.New("raft: apply method was not specified")
	// ErrPeerNotFound is thrown when we try an operation on a peer that does not exist in the cluster list
	ErrPeerNotFound = errors.New("raft: peer not found in cluster list")
	// ErrAppendEntry is thrown when the node fail to append an entry to the logs
	ErrAppendEntry = errors.New("raft: failed to append entry to logs")
	// ErrSetHardState is thrown when the node fails to set the hard state
	ErrSetHardState = errors.New("raft: failed to set the hard state for log append entry")
	// ErrApplySnapshot is thrown when the node fails to apply a snapshot
	ErrApplySnapshot = errors.New("raft: failed to apply snapshot on raft node")
	// ErrStopped is thrown when an operation was submitted but the node was stopped in the meantime
	ErrStopped = errors.New("raft: failed to process the request: node is stopped")
	// ErrRequestTooLarge is thrown when a raft internal message is too large to be sent
	ErrRequestTooLarge = errors.New("raft: raft message is too large and can't be sent")
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

	storeLock   sync.RWMutex
	PStore      map[string]string
	raftStore   *raft.MemoryStorage
	memoryStore WatchableStore
	Config      *raft.Config
	reqIDGen    *idutil.Generator
	w           wait.Wait
	wal         *wal.WAL
	snapshotter *snap.Snapshotter
	stateDir    string

	ticker *time.Ticker
	stopCh chan struct{}
	errCh  chan error

	// ApplyCommand is called when a log entry
	// is committed to the logs, behind can
	// lie any kind of logic processing the
	// message
	apply ApplyCommand
}

// NewNodeOptions provides arguments for NewNode
type NewNodeOptions struct {
	ID       uint64
	Addr     string
	Config   *raft.Config
	Apply    ApplyCommand
	StateDir string
}

// NewNode generates a new Raft node based on an unique
// ID, an address and optionally: a handler and receive
// only channel to send event when an entry is committed
// to the logs
func NewNode(ctx context.Context, opts NewNodeOptions) (*Node, error) {
	cfg := opts.Config
	if cfg == nil {
		cfg = DefaultNodeConfig()
	}

	store := raft.NewMemoryStorage()
	peers := []raft.Peer{{ID: opts.ID}}

	n := &Node{
		ID:          opts.ID,
		Ctx:         ctx,
		Cluster:     NewCluster(),
		memoryStore: NewMemoryStore(),
		raftStore:   store,
		Address:     opts.Addr,
		Config: &raft.Config{
			ID:              opts.ID,
			ElectionTick:    cfg.ElectionTick,
			HeartbeatTick:   cfg.HeartbeatTick,
			Storage:         store,
			MaxSizePerMsg:   cfg.MaxSizePerMsg,
			MaxInflightMsgs: cfg.MaxInflightMsgs,
			Logger:          cfg.Logger,
		},
		PStore:   make(map[string]string),
		ticker:   time.NewTicker(time.Second),
		stopCh:   make(chan struct{}),
		reqIDGen: idutil.NewGenerator(uint8(opts.ID), time.Now()),
		apply:    opts.Apply,
		stateDir: opts.StateDir,
	}

	if err := n.load(); err != nil {
		n.ticker.Stop()
		return nil, err
	}

	n.Cluster.AddPeer(
		&Peer{
			RaftNode: &api.RaftNode{
				ID:   opts.ID,
				Addr: opts.Addr,
			},
		},
	)

	// TODO(aaronl): This should be RestartNode in cases where the cluster
	// info has been restored from storage.
	n.Node = raft.StartNode(n.Config, peers)
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

func (n *Node) walDir() string {
	return filepath.Join(n.stateDir, "wal")
}

func (n *Node) snapDir() string {
	return filepath.Join(n.stateDir, "snap")
}

func (n *Node) load() error {
	walDir := n.walDir()
	snapDir := n.snapDir()

	haveWAL := wal.Exist(walDir)

	if err := os.MkdirAll(snapDir, 0700); err != nil {
		return fmt.Errorf("create snapshot directory error: %v", err)
	}

	// Create a snapshotter
	n.snapshotter = snap.New(snapDir)

	if !haveWAL {
		// TODO(aaronl): serialize node ID and cluster ID, and pass the
		// result in to wal.Create
		var err error
		n.wal, err = wal.Create(walDir, []byte{})
		if err != nil {
			return fmt.Errorf("create wal error: %v", err)
		}
		return nil
	}

	// Load snapshot data
	snapshot, err := n.snapshotter.Load()
	if err != nil && err != snap.ErrNoSnapshot {
		return err
	}

	if snapshot != nil {
		// Load the snapshot data into the store
		err := n.memoryStore.Restore(snapshot.Data)
		if err != nil {
			return err
		}
	}
	// Read logs to fully catch up store
	return n.readWAL(snapshot)
}

func (n *Node) readWAL(snapshot *raftpb.Snapshot) error {
	var (
		walsnap walpb.Snapshot
		err     error
		//wmetadata []byte
		st   raftpb.HardState
		ents []raftpb.Entry
	)

	if snapshot != nil {
		walsnap.Index = snapshot.Metadata.Index
		walsnap.Term = snapshot.Metadata.Term
	}

	repaired := false
	for {
		if n.wal, err = wal.Open(n.walDir(), walsnap); err != nil {
			return fmt.Errorf("open wal error: %v", err)
		}
		if /*wmetadata*/ _, st, ents, err = n.wal.ReadAll(); err != nil {
			if err := n.wal.Close(); err != nil {
				return err
			}
			// we can only repair ErrUnexpectedEOF and we never repair twice.
			if repaired || err != io.ErrUnexpectedEOF {
				return fmt.Errorf("read wal error (%v) and cannot be repaired", err)
			}
			if !wal.Repair(n.walDir()) {
				return fmt.Errorf("WAL error (%v) cannot be repaired", err)
			}
			logrus.Infof("repaired WAL error (%v)", err)
			repaired = true
			continue
		}
		break
	}
	// TODO(aaronl): deserialize metadata, and set node ID and cluster ID
	// from it.
	// TODO(aaronl): do we need to change NewNode so it doesn't take an ID?
	if err := n.raftStore.ApplySnapshot(*snapshot); err != nil {
		return err
	}
	if err := n.raftStore.SetHardState(st); err != nil {
		return err
	}
	if err := n.raftStore.Append(ents); err != nil {
		return err
	}

	return nil
}

// Start is the main loop for a Raft node, it
// goes along the state machine, acting on the
// messages received from other Raft nodes in
// the cluster
func (n *Node) Start() (errCh <-chan error) {
	n.w = wait.New()
	var err error
	n.errCh = make(chan error)
	go func() {
		for {
			select {
			case <-n.ticker.C:
				n.Tick()

			case rd := <-n.Ready():
				// Save entries to storage
				if err = n.saveToStorage(rd.HardState, rd.Entries, rd.Snapshot); err != nil {
					n.errCh <- err
				}

				// TODO(aaronl): Update the MemoryStore based
				// on the incoming message.

				// Send raft messages to peers
				if err = n.send(rd.Messages); err != nil {
					n.errCh <- err
				}

				// Process committed entries
				for _, entry := range rd.CommittedEntries {
					if err = n.processCommitted(entry); err != nil {
						n.errCh <- err
					}
				}

				// Advance the state machine
				n.Advance()

			case <-n.stopCh:
				n.Stop()
				n.Node = nil
				close(n.stopCh)
				return
			}
		}
	}()
	return n.errCh
}

// Shutdown stops the raft node processing loop.
// Calling Shutdown on an already stopped node
// will result in a deadlock
func (n *Node) Shutdown() {
	n.stopCh <- struct{}{}
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

// Join asks to a member of the raft to propose
// a configuration change and add us as a member thus
// beginning the log replication process. This method
// is called from an aspiring member to an existing member
func (n *Node) Join(ctx context.Context, req *api.JoinRequest) (*api.JoinResponse, error) {
	meta, err := proto.Marshal(req.Node)
	if err != nil {
		return nil, err
	}

	confChange := raftpb.ConfChange{
		ID:      req.Node.ID,
		Type:    raftpb.ConfChangeAddNode,
		NodeID:  req.Node.ID,
		Context: meta,
	}

	err = n.ProposeConfChange(n.Ctx, confChange)
	if err != nil {
		return nil, err
	}

	var nodes []*api.RaftNode
	for _, node := range n.Cluster.Peers() {
		nodes = append(nodes, &api.RaftNode{
			ID:   node.ID,
			Addr: node.Addr,
		})
	}

	// TODO (abronan): instead of sending back
	// the list of nodes and let the new member
	// add them itself to its local list: grpc
	// call add from the node sending the conf
	// change
	return &api.JoinResponse{Members: nodes}, nil
}

// Leave asks to a member of the raft to remove
// us from the raft cluster. This method is called
// from a member who is willing to leave its raft
// membership to an active member of the raft
func (n *Node) Leave(ctx context.Context, req *api.LeaveRequest) (*api.LeaveResponse, error) {
	confChange := raftpb.ConfChange{
		ID:      req.Node.ID,
		Type:    raftpb.ConfChangeRemoveNode,
		NodeID:  req.Node.ID,
		Context: []byte(""),
	}

	err := n.ProposeConfChange(n.Ctx, confChange)
	if err != nil {
		return nil, err
	}

	return &api.LeaveResponse{}, nil
}

// ProcessRaftMessage calls 'Step' which advances the
// raft state machine with the provided message on the
// receiving node
func (n *Node) ProcessRaftMessage(ctx context.Context, msg *api.ProcessRaftMessageRequest) (*api.ProcessRaftMessageResponse, error) {
	err := n.Step(n.Ctx, *msg.Msg)
	if err != nil {
		return nil, err
	}

	return &api.ProcessRaftMessageResponse{}, nil
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
func (n *Node) RegisterNode(node *api.RaftNode) error {
	var (
		client *Raft
		err    error
	)

	for i := 1; i <= MaxRetries; i++ {
		client, err = GetRaftClient(node.Addr, 2*time.Second)
		if err != nil {
			if i == MaxRetries {
				return err
			}
		}
	}

	n.Cluster.AddPeer(&Peer{RaftNode: node, Client: client})
	return nil
}

// RegisterNodes registers a set of nodes in the cluster
func (n *Node) RegisterNodes(nodes []*api.RaftNode) (err error) {
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
func (n *Node) UnregisterNode(id uint64) error {
	// Do not unregister yourself
	if n.ID == id {
		return nil
	}

	peer := n.Cluster.GetPeer(id)
	if peer == nil {
		return ErrPeerNotFound
	}

	err := peer.Client.Conn.Close()
	if err != nil {
		return err
	}

	n.Cluster.RemovePeer(id)
	return nil
}

// ProposeValue calls Propose on the raft and waits
// on the commit log action before returning a result
func (n *Node) ProposeValue(ctx context.Context, pair *api.Pair, timeout time.Duration) error {
	if timeout == 0 {
		ctx, _ = context.WithTimeout(ctx, defaultProposeTimeout)
	} else {
		ctx, _ = context.WithTimeout(ctx, timeout)
	}

	_, err := n.processInternalRaftRequest(ctx, &api.InternalRaftRequest{Pair: pair})
	if err != nil {
		return err
	}
	return nil
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

// Saves a log entry to our Store
func (n *Node) saveToStorage(hardState raftpb.HardState, entries []raftpb.Entry, snapshot raftpb.Snapshot) (err error) {
	// TODO(aaronl): Should this move to the end of the function?
	if err = n.raftStore.Append(entries); err != nil {
		return ErrAppendEntry
	}

	if !raft.IsEmptyHardState(hardState) {
		if err = n.raftStore.SetHardState(hardState); err != nil {
			return ErrSetHardState
		}
	}

	if !raft.IsEmptySnap(snapshot) {
		if err := n.saveSnapshot(snapshot); err != nil {
			return ErrApplySnapshot
		}
		if err = n.raftStore.ApplySnapshot(snapshot); err != nil {
			return ErrApplySnapshot
		}
	}

	if err := n.wal.Save(hardState, entries); err != nil {
		// TODO(aaronl): These error types should really wrap more
		// detailed errors.
		return ErrApplySnapshot
	}

	// TODO(aaronl): trigger a snapshot every once in awhile

	return nil
}

func (n *Node) saveSnapshot(snapshot raftpb.Snapshot) error {
	err := n.wal.SaveSnapshot(walpb.Snapshot{
		Index: snapshot.Metadata.Index,
		Term:  snapshot.Metadata.Term,
	})
	if err != nil {
		return err
	}
	err = n.snapshotter.SaveSnap(snapshot)
	if err != nil {
		return err
	}
	err = n.wal.ReleaseLockTo(snapshot.Metadata.Index)
	if err != nil {
		return err
	}
	return nil
}

// Sends a series of messages to members in the raft
func (n *Node) send(messages []raftpb.Message) error {
	peers := n.Cluster.Peers()

	for _, m := range messages {
		// Process locally
		if m.To == n.ID {
			if err := n.Step(n.Ctx, m); err != nil {
				return err
			}
			continue
		}

		// If node is an active raft member send the message
		if peer, ok := peers[m.To]; ok {
			_, err := peer.Client.ProcessRaftMessage(n.Ctx, &api.ProcessRaftMessageRequest{Msg: &m})
			if err != nil {
				n.ReportUnreachable(peer.ID)
			}
		}
	}

	return nil
}

type applyResult struct {
	resp proto.Message
	err  error
}

func (n *Node) processInternalRaftRequest(ctx context.Context, r *api.InternalRaftRequest) (proto.Message, error) {
	r.ID = n.reqIDGen.Next()

	ch := n.w.Register(r.ID)

	data, err := r.Marshal()
	if err != nil {
		return nil, err
	}

	if len(data) > maxRequestBytes {
		return nil, ErrRequestTooLarge
	}

	err = n.Propose(ctx, data)
	if err != nil {
		return nil, err
	}

	select {
	case x := <-ch:
		res := x.(*applyResult)
		return res.resp, res.err
	case <-ctx.Done():
		n.w.Trigger(r.ID, nil)
		return nil, ctx.Err()
	case <-n.stopCh:
		return nil, ErrStopped
	}
}

func (n *Node) processCommitted(entry raftpb.Entry) error {
	// Process a normal entry
	if entry.Type == raftpb.EntryNormal && entry.Data != nil {
		if err := n.processEntry(entry); err != nil {
			return err
		}
	}

	// Process a configuration change (add/remove node)
	if entry.Type == raftpb.EntryConfChange {
		if err := n.processConfChange(entry); err != nil {
			return err
		}
	}

	return nil
}

func (n *Node) processEntry(entry raftpb.Entry) error {
	r := &api.InternalRaftRequest{}
	err := proto.Unmarshal(entry.Data, r)
	if err != nil {
		return err
	}

	if n.apply != nil {
		n.apply(entry.Data)
	}

	n.Put(r.Pair.Key, string(r.Pair.Value))
	n.w.Trigger(r.ID, &applyResult{resp: r, err: nil})
	return nil
}

func (n *Node) processConfChange(conf raftpb.Entry) error {
	var (
		err error
		cc  raftpb.ConfChange
	)

	if err = cc.Unmarshal(conf.Data); err != nil {
		return err
	}

	switch cc.Type {
	case raftpb.ConfChangeAddNode:
		err = n.applyAddNode(cc)
	case raftpb.ConfChangeRemoveNode:
		err = n.applyRemoveNode(cc)
	}

	if err != nil {
		return err
	}

	n.ApplyConfChange(cc)
	return nil
}

// applyAddNode is called when we receive a ConfChange
// from a member in the raft cluster, this adds a new
// node to the existing raft cluster
func (n *Node) applyAddNode(conf raftpb.ConfChange) error {
	peer := &api.RaftNode{}
	err := proto.Unmarshal(conf.Context, peer)
	if err != nil {
		return err
	}

	// ID must be non zero
	if peer.ID == 0 {
		return nil
	}

	if n.ID != peer.ID {
		if err = n.RegisterNode(peer); err != nil {
			return err
		}
	}
	return nil
}

// applyRemoveNode is called when we receive a ConfChange
// from a member in the raft cluster, this removes a node
// from the existing raft cluster
func (n *Node) applyRemoveNode(conf raftpb.ConfChange) (err error) {
	// The leader steps down
	if n.ID == n.Leader() && n.ID == conf.NodeID {
		n.Stop()
		return
	}

	// If the node from where the remove is issued is
	// a follower and the leader steps down, Campaign
	// to be the leader
	if conf.NodeID == n.Leader() {
		if err = n.Campaign(n.Ctx); err != nil {
			return err
		}
	}

	// Unregister the node
	if err = n.UnregisterNode(conf.NodeID); err != nil {
		return err
	}

	return nil
}

func (n *Node) processSnapshot(snapshot raftpb.Snapshot) {
	// TODO(abronan): implement snapshot
	panic(fmt.Sprintf("Applying snapshot on node %v is not implemented", n.ID))
}
