package dispatcher

import (
	"errors"
	"fmt"
	"sync"
	"time"

	log "github.com/Sirupsen/logrus"
	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/manager/state"
	"github.com/docker/swarm-v2/manager/state/watch"
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
	mu               sync.Mutex
	addr             string
	nodes            *nodeStore
	store            state.WatchableStore
	mgrQueue         *watch.Queue
	lastSeenManagers []*api.WeightedPeer
	config           *Config
}

// New returns Dispatcher with store.
func New(store state.WatchableStore, c *Config) *Dispatcher {
	return &Dispatcher{
		addr:     c.Addr,
		nodes:    newNodeStore(c.HeartbeatPeriod, c.HeartbeatEpsilon, c.GracePeriodMultiplier),
		store:    store,
		mgrQueue: watch.NewQueue(16),
		config:   c,
		lastSeenManagers: []*api.WeightedPeer{
			{
				Addr:   c.Addr, // TODO: change after raft
				Weight: 1,
			},
		},
	}
}

// Register is used for registration of node with particular dispatcher.
func (d *Dispatcher) Register(ctx context.Context, r *api.RegisterRequest) (*api.RegisterResponse, error) {
	log.WithField("request", r).Debugf("(*Dispatcher).Register")
	// TODO: here goes auth

	// create or update node in store
	// TODO(stevvooe): Validate node specification.
	var node *api.Node
	err := d.store.Update(func(tx state.Tx) error {
		node = tx.Nodes().Get(r.NodeID)
		if node != nil {
			node.Description = r.Description
			node.Status = api.NodeStatus{
				State: api.NodeStatus_READY,
			}
			return tx.Nodes().Update(node)
		}

		node = &api.Node{
			ID:          r.NodeID,
			Description: r.Description,
			Status: api.NodeStatus{
				State: api.NodeStatus_READY,
			},
		}
		return tx.Nodes().Create(node)
	})
	if err != nil {
		return nil, err
	}

	nid := node.ID // prevent the closure from holding onto the entire Node.

	expireFunc := func() {
		nodeStatus := api.NodeStatus{State: api.NodeStatus_DOWN, Message: "heartbeat failure"}
		log.WithField("node.id", nid).Debugf("heartbeat expiration")
		if err := d.nodeRemove(nid, nodeStatus); err != nil {
			log.Errorf("error deregistering node %s after heartbeat expiration: %v", nid, err)
		}
	}

	rn := d.nodes.Add(node, expireFunc)

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
			return stream.Context().Err()
		}
	}
}

func (d *Dispatcher) nodeRemove(id string, status api.NodeStatus) error {
	err := d.store.Update(func(tx state.Tx) error {
		node := tx.Nodes().Get(id)
		if node == nil {
			return errors.New("node not found")
		}
		node.Status = status
		return tx.Nodes().Update(node)
	})
	if err != nil {
		return fmt.Errorf("failed to update node %s status to down: %v", id, err)
	}

	if rn := d.nodes.Delete(id); rn == nil {
		return fmt.Errorf("node %s is not found in local storage", id)
	}

	return nil
}

// Heartbeat is heartbeat method for nodes. It returns new TTL in response.
// Node should send new heartbeat earlier than now + TTL, otherwise it will
// be deregistered from dispatcher and its status will be updated to NodeStatus_DOWN
func (d *Dispatcher) Heartbeat(ctx context.Context, r *api.HeartbeatRequest) (*api.HeartbeatResponse, error) {
	log.WithField("request", r).Debugf("(*Dispatcher).Heartbeat")

	period, err := d.nodes.Heartbeat(r.NodeID, r.SessionID)
	return &api.HeartbeatResponse{Period: period}, err
}

func (d *Dispatcher) watchManagers() {
	publish := func() {
		mgrs := []*api.WeightedPeer{
			{
				Addr:   d.addr, // TODO: change after raft
				Weight: 1,
			},
		}
		d.mu.Lock()
		d.lastSeenManagers = mgrs
		d.mu.Unlock()
		d.mgrQueue.Publish(watch.Event{Payload: mgrs})
	}
	publish()
	// TODO: here should be code which asks leader about managers with their weights
	for range time.Tick(1 * time.Second) {
		publish()
	}
}

func (d *Dispatcher) getManagers() []*api.WeightedPeer {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.lastSeenManagers
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

	if err := stream.Send(&api.SessionMessage{
		Managers:   d.getManagers(),
		Disconnect: false,
	}); err != nil {
		return err
	}

	mgrUpdates := d.mgrQueue.Watch()

	for {
		// After each message send, we need to check the nodes sessionID hasn't
		// changed. If it has, we will the stream and make the node
		// re-register.
		node, err := d.nodes.GetWithSession(r.NodeID, r.SessionID)
		if err != nil {
			return err
		}
		var (
			disconnect bool
			mgrs       []*api.WeightedPeer
		)
		select {
		case <-node.Disconnect:
			disconnect = true
		case ev := <-mgrUpdates:
			mgrs = ev.Payload.([]*api.WeightedPeer)
		case <-stream.Context().Done():
			return stream.Context().Err()
		}
		if mgrs == nil {
			mgrs = d.getManagers()
		}
		if disconnect {
			nodeStatus := api.NodeStatus{State: api.NodeStatus_DISCONNECTED, Message: "node is currently trying to find new manager"}
			if err := d.nodeRemove(r.NodeID, nodeStatus); err != nil {
				log.Error(err)
			}
		}

		if err := stream.Send(&api.SessionMessage{
			Managers:   mgrs,
			Disconnect: disconnect,
		}); err != nil {
			return err
		}

		time.Sleep(5 * time.Second) // TODO(stevvooe): This should really be watch activated.
	}
}

// NodeCount returns number of nodes which connected to this dispatcher.
func (d *Dispatcher) NodeCount() int {
	return d.nodes.Len()
}
