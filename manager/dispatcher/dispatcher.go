package dispatcher

import (
	"errors"
	"fmt"
	"log"
	"math/rand"
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

const (
	defaultHeartBeatPeriod       = 5 * time.Second
	defaultHeartBeatEpsilon      = 500 * time.Millisecond
	defaultGracePeriodMultiplier = 4
)

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

type periodChooser struct {
	period  time.Duration
	epsilon time.Duration
	rand    *rand.Rand
}

func newPeriodChooser(period, eps time.Duration) *periodChooser {
	return &periodChooser{
		period:  period,
		epsilon: eps,
		rand:    rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

func (pc *periodChooser) Choose() time.Duration {
	var adj int64
	if pc.epsilon > 0 {
		adj = rand.Int63n(int64(2*pc.epsilon)) - int64(pc.epsilon)
	}
	return pc.period + time.Duration(adj)
}

// Config is configuration for Dispatcher. For default you should use
// DefautConfig.
type Config struct {
	HeartbeatPeriod       time.Duration
	HeartbeatEpsilon      time.Duration
	GracePeriodMultiplier int
}

// DefaultConfig returns default config for Dispatcher.
func DefaultConfig() *Config {
	return &Config{
		HeartbeatPeriod:       defaultHeartBeatPeriod,
		HeartbeatEpsilon:      defaultHeartBeatEpsilon,
		GracePeriodMultiplier: defaultGracePeriodMultiplier,
	}
}

// Dispatcher is responsible for dispatching tasks and tracking agent health.
type Dispatcher struct {
	mu                    sync.Mutex
	nodes                 map[string]*registeredNode
	store                 state.WatchableStore
	gracePeriodMultiplier int
	periodChooser         *periodChooser
}

// New returns Dispatcher with store.
func New(store state.WatchableStore, c *Config) *Dispatcher {
	return &Dispatcher{
		nodes:                 make(map[string]*registeredNode),
		store:                 store,
		periodChooser:         newPeriodChooser(c.HeartbeatPeriod, c.HeartbeatEpsilon),
		gracePeriodMultiplier: c.GracePeriodMultiplier,
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

	err := d.store.Update(func(tx state.Tx) error {
		err := tx.Nodes().Create(n)
		if err != nil {
			if err != state.ErrExist {
				return err
			}
			if err := tx.Nodes().Update(n); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	d.mu.Lock()
	ttl := d.periodChooser.Choose()
	d.nodes[n.Spec.ID] = &registeredNode{
		Heartbeat: heartbeat.New(ttl*time.Duration(d.gracePeriodMultiplier), func() {
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
	err := d.store.Update(func(tx state.Tx) error {
		for _, t := range r.Tasks {
			if err := tx.Tasks().Update(&api.Task{ID: t.ID, Status: t.Status}); err != nil {
				return err
			}
		}
		return nil
	})

	if err != nil {
		return nil, err
	}
	return nil, nil
}

// Tasks is a stream of tasks state for node. Each message contains full list
// of tasks which should be run on node, if task is not present in that list,
// it should be terminated.
func (d *Dispatcher) Tasks(r *api.TasksRequest, stream api.Dispatcher_TasksServer) error {
	log.Print("Tasks", r)
	d.mu.Lock()
	_, ok := d.nodes[r.NodeID]
	d.mu.Unlock()
	if !ok {
		return grpc.Errorf(codes.NotFound, ErrNodeNotRegistered.Error())
	}

	// Before each message send, we need to check the nodes sessionID hasn't
	// changed. If it has, we will the stream and make the node
	// re-register.
	if rn.SessionID != r.SessionID {
		rn.mu.Unlock()
		return grpc.Errorf(codes.InvalidArgument, ErrSessionInvalid.Error())
	}
	rn.mu.Unlock()

	watchQueue := d.store.WatchQueue()
	nodeTasks := state.Watch(watchQueue,
		state.EventCreateTask{Task: &api.Task{NodeID: r.NodeID},
			Checks: []state.TaskCheckFunc{state.TaskCheckNodeID}},
		state.EventUpdateTask{Task: &api.Task{NodeID: r.NodeID},
			Checks: []state.TaskCheckFunc{state.TaskCheckNodeID}},
		state.EventDeleteTask{Task: &api.Task{NodeID: r.NodeID},
			Checks: []state.TaskCheckFunc{state.TaskCheckNodeID}})
	defer watchQueue.StopWatch(nodeTasks)

	tasksMap := make(map[string]*api.Task)
	err := d.store.View(func(readTx state.ReadTx) error {
		tasks, err := readTx.Tasks().Find(state.ByNodeID(r.NodeID))
		if err != nil {
			return nil
		}
		if err := stream.Send(&api.TasksMessage{Tasks: tasks}); err != nil {
			return err
		}
		for _, t := range tasks {
			tasksMap[t.ID] = t
		}
		return nil
	})
	if err != nil {
		return err
	}

	for {
		select {
		case event := <-nodeTasks:
			switch v := event.Payload.(type) {
			case state.EventCreateTask:
				tasksMap[v.Task.ID] = v.Task
			case state.EventUpdateTask:
				tasksMap[v.Task.ID] = v.Task
			case state.EventDeleteTask:
				delete(tasksMap, v.Task.ID)
			}
		case <-stream.Context().Done():
			return nil
		}
		var tasks []*api.Task
		for _, t := range tasksMap {
			tasks = append(tasks, t)
		}
		if err := stream.Send(&api.TasksMessage{Tasks: tasks}); err != nil {
			return err
		}
	}
}

func (d *Dispatcher) nodeDown(id string) error {
	d.mu.Lock()
	delete(d.nodes, id)
	d.mu.Unlock()

	err := d.store.Update(func(tx state.Tx) error {
		return tx.Nodes().Update(&api.Node{
			Spec:   &api.NodeSpec{ID: id},
			Status: api.NodeStatus{State: api.NodeStatus_DOWN},
		})
	})
	if err != nil {
		return fmt.Errorf("failed to update node %s status to down", id)
	}
	return nil
}

// Heartbeat is heartbeat method for nodes. It returns new TTL in response.
// Node should send new heartbeat earlier than now + TTL, otherwise it will
// be deregistered from dispatcher and its status will be updated to NodeStatus_DOWN
func (d *Dispatcher) Heartbeat(ctx context.Context, r *api.HeartbeatRequest) (*api.HeartbeatResponse, error) {
	d.mu.Lock()
	node, ok := d.nodes[r.NodeID]
	if !ok {
		d.mu.Unlock()
		return nil, grpc.Errorf(codes.NotFound, ErrNodeNotRegistered.Error())
	}
	ttl := d.periodChooser.Choose()
	d.mu.Unlock()
	node.Heartbeat.Update(ttl)
	node.Heartbeat.Beat()
	return &api.HeartbeatResponse{Period: period}, nil
}

func (d *Dispatcher) getManagers() []*api.WeightedPeer {
	return []*api.WeightedPeer{
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
func (d *Dispatcher) Session(r *api.SessionRequest, stream api.Dispatcher_SessionServer) error {
	d.mu.Lock()
	_, ok := d.nodes[r.NodeID]
	d.mu.Unlock()
	if !ok {
		return grpc.Errorf(codes.NotFound, ErrNodeNotRegistered.Error())
	}
	for {
		if err := stream.Send(&api.SessionMessage{
			Managers:   d.getManagers(),
			Disconnect: false,
		}); err != nil {
			return err
		}
	}
}
