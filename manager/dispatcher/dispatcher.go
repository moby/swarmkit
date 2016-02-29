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
	_, ok := d.nodes[r.Spec.ID]
	d.mu.Unlock()
	if ok {
		return nil, grpc.Errorf(codes.AlreadyExists, ErrNodeAlreadyRegistered.Error())
	}

	n := &api.Node{
		Spec: r.Spec,
	}

	n.Status.State = api.NodeStatus_READY
	// create or update node in raft
	err := d.store.CreateNode(n.Spec.ID, n)
	if err != nil {
		if err != state.ErrExist {
			return nil, err
		}
		if err := d.store.UpdateNode(n.Spec.ID, n); err != nil {
			return nil, err
		}
	}
	ttl := d.electTTL()
	d.mu.Lock()

	d.nodes[n.Spec.ID] = &registeredNode{
		Heartbeat: heartbeat.New(ttl, func() {
			if err := d.nodeDown(n.Spec.ID); err != nil {
				logrus.Errorf("error deregistering node %s after heartbeat was not received: %v", n.Spec.ID, err)
			}
		}),
		Node: n,
	}
	d.mu.Unlock()
	return &api.RegisterResponse{TTL: ttl}, nil
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
		if err := d.store.UpdateTask(t.ID, &api.Task{Status: t.Status}); err != nil {
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
		tasks := d.store.TasksByNode(r.NodeID)
		if len(tasks) != 0 {
			if err := stream.Send(&api.TasksResponse{Tasks: d.store.TasksByNode(r.NodeID)}); err != nil {
				return err
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func (d *Dispatcher) nodeDown(id string) error {
	d.mu.Lock()
	delete(d.nodes, id)
	d.mu.Unlock()

	update := &api.Node{
		Spec: &api.NodeSpec{ID: id},
	}
	update.Status.State = api.NodeStatus_DOWN

	if err := d.store.UpdateNode(id, update); err != nil {
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
