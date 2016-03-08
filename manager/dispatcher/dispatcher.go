package dispatcher

import (
	"errors"
	"fmt"
	"sync"
	"time"

	log "github.com/Sirupsen/logrus"
	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/identity"
	"github.com/docker/swarm-v2/pkg/heartbeat"
	"github.com/docker/swarm-v2/state"
	"golang.org/x/net/context"
)

const (
	defaultHeartBeatPeriod       = 5 * time.Second
	defaultHeartBeatEpsilon      = 500 * time.Millisecond
	defaultGracePeriodMultiplier = 3
)

var (
	// ErrNodeAlreadyRegistered returned if node with same ID was already
	// registered with this dispatcher.
	ErrNodeAlreadyRegistered = errors.New("node already registered")
	// ErrNodeNotRegistered returned if node with such ID wasn't registered
	// with this dispatcher.
	ErrNodeNotRegistered = errors.New("node not registered")
	// ErrSessionInvalid returned when the session in use is no longer valid.
	// The node should re-register and start a new session.
	ErrSessionInvalid = errors.New("session invalid")
)

// Config is configuration for Dispatcher. For default you should use
// DefautConfig.
type Config struct {
	// Addr configures the address the dispatcher reports to agents.
	Addr                  string
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
	addr                  string
	nodes                 *nodeStore
	store                 state.WatchableStore
	gracePeriodMultiplier int
	periodChooser         *periodChooser
}

// New returns Dispatcher with store.
func New(store state.WatchableStore, c *Config) *Dispatcher {
	return &Dispatcher{
		addr:                  c.Addr,
		nodes:                 newNodeStore(),
		store:                 store,
		periodChooser:         newPeriodChooser(c.HeartbeatPeriod, c.HeartbeatEpsilon),
		gracePeriodMultiplier: c.GracePeriodMultiplier,
	}
}

// Register is used for registration of node with particular dispatcher.
func (d *Dispatcher) Register(ctx context.Context, r *api.RegisterRequest) (*api.RegisterResponse, error) {
	log.WithField("request", r).Debugf("(*Dispatcher).Register")

	// TODO: here goes auth

	// TODO(stevvooe): Validate node specification.
	n := &api.Node{
		Spec: r.Spec,
		ID:   r.NodeID,
	}
	nid := n.ID // prevent the closure from holding onto the entire Node.

	rn := &registeredNode{
		SessionID: identity.NewID(), // session ID is local to the dispatcher.
		Heartbeat: heartbeat.New(d.periodChooser.Choose()*time.Duration(d.gracePeriodMultiplier), func() {
			if err := d.nodeDown(nid); err != nil {
				log.Errorf("error deregistering node %s after heartbeat was not received: %v", nid, err)
			}
		}),
		Node: n,
	}

	d.nodes.Add(rn)

	// create or update node in raft
	rn.mu.Lock()
	defer rn.mu.Unlock()
	rn.Node.Status.State = api.NodeStatus_READY
	if err := d.store.Update(func(tx state.Tx) error {
		err := tx.Nodes().Create(rn.Node)
		if err != nil {
			if err != state.ErrExist {
				return err
			}
			if err := tx.Nodes().Update(rn.Node); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return nil, err
	}

	// NOTE(stevvooe): We need be a little careful with re-registration. The
	// current implementation just matches the node id and then gives away the
	// sessionID. If we ever want to use sessionID as a secret, which we may
	// want to, this is giving away the keys to the kitchen.
	//
	// The right behavior is going to be informed by identity. Basically, each
	// time a node registers, we invalidate the session and issue a new
	// session, once identity is proven. This will cause misbehaved agents to
	// be kicked when multiple connections are made.
	return &api.RegisterResponse{NodeID: rn.Node.ID, SessionID: rn.SessionID}, nil
}

// UpdateTaskStatus updates status of task. Node should send such updates
// on every status change of its tasks.
func (d *Dispatcher) UpdateTaskStatus(ctx context.Context, r *api.UpdateTaskStatusRequest) (*api.UpdateTaskStatusResponse, error) {
	log.WithField("request", r).Debugf("(*Dispatcher).UpdateTaskStatus")

	if _, err := d.nodes.GetWithSession(r.NodeID, r.SessionID); err != nil {
		return nil, err
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
	log.WithField("request", r).Debugf("(*Dispatcher).Tasks")

	if _, err := d.nodes.GetWithSession(r.NodeID, r.SessionID); err != nil {
		return err
	}

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
		for _, t := range tasks {
			tasksMap[t.ID] = t
		}
		return nil
	})
	if err != nil {
		return err
	}

	for {
		if _, err := d.nodes.GetWithSession(r.NodeID, r.SessionID); err != nil {
			return err
		}

		var tasks []*api.Task
		for _, t := range tasksMap {
			tasks = append(tasks, t)
		}

		if err := stream.Send(&api.TasksMessage{Tasks: tasks}); err != nil {
			return err
		}

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
	}
}

func (d *Dispatcher) nodeDown(id string) error {
	d.nodes.Delete(id)

	err := d.store.Update(func(tx state.Tx) error {
		return tx.Nodes().Update(&api.Node{
			ID:     id,
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
	log.WithField("request", r).Debugf("(*Dispatcher).Heartbeat")

	rn, err := d.nodes.GetWithSession(r.NodeID, r.SessionID)
	if err != nil {
		return nil, err
	}

	period := d.periodChooser.Choose() // base period for node
	grace := period * time.Duration(d.gracePeriodMultiplier)
	rn.Heartbeat.Update(grace)
	rn.Heartbeat.Beat()
	return &api.HeartbeatResponse{Period: period}, nil
}

func (d *Dispatcher) getManagers() []*api.WeightedPeer {
	return []*api.WeightedPeer{
		{
			Addr:   d.addr, // TODO: change after raft
			Weight: 1,
		},
	}
}

// Session is stream which controls agent connection.
// Each message contains list of backup Managers with weights. Also there is
// special boolean field Disconnect which if true indicates that node should
// reconnect to another Manager immediately.
func (d *Dispatcher) Session(r *api.SessionRequest, stream api.Dispatcher_SessionServer) error {
	log.WithField("request", r).Debugf("(*Dispatcher).Session")
	if _, err := d.nodes.GetWithSession(r.NodeID, r.SessionID); err != nil {
		return err
	}

	for {
		// After each message send, we need to check the nodes sessionID hasn't
		// changed. If it has, we will the stream and make the node
		// re-register.
		if _, err := d.nodes.GetWithSession(r.NodeID, r.SessionID); err != nil {
			return err
		}

		if err := stream.Send(&api.SessionMessage{
			Managers:   d.getManagers(),
			Disconnect: false,
		}); err != nil {
			return err
		}

		time.Sleep(5 * time.Second) // TODO(stevvooe): This should really be watch activated.
	}
}
