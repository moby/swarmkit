package dispatcher

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"

	"github.com/Sirupsen/logrus"
	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/pkg/heartbeat"
	"github.com/docker/swarm-v2/state"
	"golang.org/x/net/context"
)

var defaultTTL = 5 * time.Second

type registeredNode struct {
	Heartbeat *heartbeat.Heartbeat
	Tasks     []string
	Node      *api.Node
}

var (
	// ErrNodeAlreadyRegistered returned if node with same ID was already
	// registered with this dispatcher.
	ErrNodeAlreadyRegistered = errors.New("node already registered")
	// ErrNodeNotRegistered returned if node with such ID wasn't registered
	// with this dispatcher.
	ErrNodeNotRegistered = errors.New("node not registered")
)

// Dispatcher is responsible for dispatching tasks and tracking agent health.
type Dispatcher struct {
	mu    sync.Mutex
	nodes map[string]*registeredNode
	store state.Store
}

// New returns Dispatcher with store.
func New(store state.Store) *Dispatcher {
	return &Dispatcher{
		nodes: make(map[string]*registeredNode),
		store: store,
	}
}

// Register is used for registration of node with particular dispatcher.
func (d *Dispatcher) Register(ctx context.Context, r *api.RegisterRequest) (*api.RegisterResponse, error) {
	d.mu.Lock()
	_, ok := d.nodes[r.Node.Id]
	d.mu.Unlock()
	if ok {
		return nil, grpc.Errorf(codes.AlreadyExists, ErrNodeAlreadyRegistered.Error())
	}
	n := r.Node
	n.Status = api.NodeStatus_READY
	// create or update node in raft
	err := d.store.CreateNode(n.Id, n)
	if err != nil {
		if err != state.ErrExist {
			return nil, err
		}
		if err := d.store.UpdateNode(n.Id, n); err != nil {
			return nil, err
		}
	}
	ttl := d.electTTL()
	d.mu.Lock()
	d.nodes[n.Id] = &registeredNode{
		Heartbeat: heartbeat.New(ttl, func() {
			if err := d.nodeDown(n.Id); err != nil {
				logrus.Errorf("error deregistering node %s after heartbeat was not received: %v", n.Id, err)
			}
		}),
		Node: n,
	}
	d.mu.Unlock()
	return &api.RegisterResponse{TTL: ttl}, nil
}

// UpdateNodeStatus updates status of particular node. Nodes can use it
// for notifying about graceful shutdowns for example.
func (d *Dispatcher) UpdateNodeStatus(context.Context, *api.UpdateNodeStatusRequest) (*api.UpdateNodeStatusResponse, error) {
	return nil, nil
}

// UpdateTaskStatus updates status of task. Node should send such updates
// on every status change of its tasks.
func (d *Dispatcher) UpdateTaskStatus(ctx context.Context, r *api.UpdateTaskStatusRequest) (*api.UpdateTaskStatusResponse, error) {
	d.mu.Lock()
	_, ok := d.nodes[r.NodeID]
	d.mu.Unlock()
	if !ok {
		return nil, grpc.Errorf(codes.NotFound, ErrNodeNotRegistered.Error())
	}
	for _, t := range r.Tasks {
		if err := d.store.UpdateTask(t.Id, &api.Task{Status: t.Status}); err != nil {
			return nil, err
		}
	}
	return nil, nil
}

// Tasks is a stream of tasks state for node. Each message contains full list
// of tasks which should be run on node, if task is not present in that list,
// it should be terminated.
func (d *Dispatcher) Tasks(r *api.TasksRequest, stream api.Agent_TasksServer) error {
	d.mu.Lock()
	_, ok := d.nodes[r.NodeID]
	d.mu.Unlock()
	if !ok {
		return grpc.Errorf(codes.NotFound, ErrNodeNotRegistered.Error())
	}
	for {
		if err := stream.Send(&api.TasksResponse{Tasks: d.store.TasksByNode(r.NodeID)}); err != nil {
			return err
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func (d *Dispatcher) nodeDown(id string) error {
	d.mu.Lock()
	delete(d.nodes, id)
	d.mu.Unlock()
	if err := d.store.UpdateNode(id, &api.Node{Id: id, Status: api.NodeStatus_DOWN}); err != nil {
		return fmt.Errorf("failed to update node %s status to down", id)
	}
	return nil
}

func (d *Dispatcher) electTTL() time.Duration {
	return defaultTTL
}

// Heartbeat is heartbeat method for nodes. It returns new TTL in response.
// Node should send new heartbeat earlier than now + TTL, otherwise it will
// be deregistered from dispatcher and its status will be updated to NodeStatus_DOWN
func (d *Dispatcher) Heartbeat(ctx context.Context, r *api.HeartbeatRequest) (*api.HeartbeatResponse, error) {
	d.mu.Lock()
	node, ok := d.nodes[r.NodeID]
	d.mu.Unlock()
	if !ok {
		return nil, grpc.Errorf(codes.NotFound, ErrNodeNotRegistered.Error())
	}
	ttl := d.electTTL()
	node.Heartbeat.Update(ttl)
	node.Heartbeat.Beat()
	return &api.HeartbeatResponse{TTL: ttl}, nil
}

func (d *Dispatcher) getManagers() []*api.ManagerInfo {
	return []*api.ManagerInfo{
		{
			Addr:   "127.0.0.1", // TODO: change after raft
			Weight: 1,
		},
	}
}

// Session is stream which controls agent connection.
// Each message contains list of backup Managers with weights. Also there is
// special boolean field Disconnect which if true indicates that node should
// reconnect to another Manager immediately.
func (d *Dispatcher) Session(r *api.SessionRequest, stream api.Agent_SessionServer) error {
	d.mu.Lock()
	_, ok := d.nodes[r.NodeID]
	d.mu.Unlock()
	if !ok {
		return grpc.Errorf(codes.NotFound, ErrNodeNotRegistered.Error())
	}
	for {
		if err := stream.Send(&api.SessionResponse{
			Managers:   d.getManagers(),
			Disconnect: false,
		}); err != nil {
			return err
		}
	}
}
