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
	"github.com/docker/swarm-v2/ca"
	"github.com/docker/swarm-v2/log"
	"github.com/docker/swarm-v2/manager/state"
	"github.com/docker/swarm-v2/manager/state/store"
	"github.com/docker/swarm-v2/manager/state/watch"
	"golang.org/x/net/context"
)

const (
	// DefaultHeartBeatPeriod is used for setting default value in cluster config
	// and in case if cluster config is missing.
	DefaultHeartBeatPeriod       = 5 * time.Second
	defaultHeartBeatEpsilon      = 500 * time.Millisecond
	defaultGracePeriodMultiplier = 3

	// maxBatchItems is the threshold of queued writes that should
	// trigger an actual transaction to commit them to the shared store.
	maxBatchItems = 10000

	// maxBatchInterval needs to strike a balance between keeping
	// latency low, and realizing opportunities to combine many writes
	// into a single transaction. A fraction of a second feels about
	// right.
	maxBatchInterval = 100 * time.Millisecond
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
		HeartbeatPeriod:       DefaultHeartBeatPeriod,
		HeartbeatEpsilon:      defaultHeartBeatEpsilon,
		GracePeriodMultiplier: defaultGracePeriodMultiplier,
	}
}

// Cluster is interface which represent raft cluster. mananger/state/raft.Node
// is implenents it. This interface needed only for easier unit-testing.
type Cluster interface {
	GetMemberlist() map[uint64]*api.RaftMember
	MemoryStore() *store.MemoryStore
}

// Dispatcher is responsible for dispatching tasks and tracking agent health.
type Dispatcher struct {
	mu               sync.Mutex
	addr             string
	nodes            *nodeStore
	store            *store.MemoryStore
	mgrQueue         *watch.Queue
	lastSeenManagers []*api.WeightedPeer
	config           *Config
	cluster          Cluster
	ctx              context.Context
	cancel           context.CancelFunc
	wg               sync.WaitGroup

	taskUpdates     map[string]*api.TaskStatus // indexed by task ID
	taskUpdatesLock sync.Mutex

	processTaskUpdatesTrigger chan struct{}
}

// New returns Dispatcher with cluster interface(usually raft.Node).
// NOTE: each handler which does something with raft must add to Dispatcher.wg
func New(cluster Cluster, c *Config) *Dispatcher {
	return &Dispatcher{
		addr:     c.Addr,
		nodes:    newNodeStore(c.HeartbeatPeriod, c.HeartbeatEpsilon, c.GracePeriodMultiplier),
		store:    cluster.MemoryStore(),
		cluster:  cluster,
		mgrQueue: watch.NewQueue(16),
		lastSeenManagers: []*api.WeightedPeer{
			{
				Addr:   c.Addr,
				Weight: 1,
			},
		},
		taskUpdates:               make(map[string]*api.TaskStatus),
		processTaskUpdatesTrigger: make(chan struct{}, 1),
		config: c,
	}
}

// Run runs dispatcher tasks which should be run on leader dispatcher.
// Dispatcher can be stopped with cancelling ctx or calling Stop().
func (d *Dispatcher) Run(ctx context.Context) error {
	d.mu.Lock()
	if d.isRunning() {
		d.mu.Unlock()
		return fmt.Errorf("dispatcher is stopped")
	}
	d.wg.Add(1)
	defer d.wg.Done()
	logger := log.G(ctx).WithField("module", "dispatcher")
	ctx = log.WithLogger(ctx, logger)
	if err := d.markNodesUnknown(ctx); err != nil {
		logger.Errorf("failed to mark all nodes unknown: %v", err)
	}
	d.store.View(func(readTx store.ReadTx) {
		clusters, err := store.FindClusters(readTx, store.ByName(store.DefaultClusterName))
		if err == nil && len(clusters) == 1 {
			d.config.HeartbeatPeriod = time.Duration(clusters[0].Spec.Dispatcher.HeartbeatPeriod)
		}
	})
	d.ctx, d.cancel = context.WithCancel(ctx)
	d.mu.Unlock()

	publishManagers := func() {
		members := d.cluster.GetMemberlist()
		var mgrs []*api.WeightedPeer
		for _, m := range members {
			mgrs = append(mgrs, &api.WeightedPeer{
				Addr:   m.Addr,
				Weight: 1,
			})
		}
		d.mu.Lock()
		d.lastSeenManagers = mgrs
		d.mu.Unlock()
		d.mgrQueue.Publish(mgrs)
	}

	publishManagers()
	publishTicker := time.NewTicker(1 * time.Second)
	defer publishTicker.Stop()

	batchTimer := time.NewTimer(maxBatchInterval)
	defer batchTimer.Stop()

	configWatcher, cancel := state.Watch(d.store.WatchQueue(), state.EventUpdateCluster{})
	defer cancel()

	for {
		select {
		case <-publishTicker.C:
			publishManagers()
		case <-d.processTaskUpdatesTrigger:
			d.processTaskUpdates()
			batchTimer.Reset(maxBatchInterval)
		case <-batchTimer.C:
			d.processTaskUpdates()
			batchTimer.Reset(maxBatchInterval)
		case v := <-configWatcher:
			cluster := v.(state.EventUpdateCluster)
			d.mu.Lock()
			d.config.HeartbeatPeriod = time.Duration(cluster.Cluster.Spec.Dispatcher.HeartbeatPeriod)
			d.nodes.updatePeriod(d.config.HeartbeatPeriod, d.config.HeartbeatEpsilon, d.config.GracePeriodMultiplier)
			d.mu.Unlock()
		case <-d.ctx.Done():
			return nil
		}
	}
}

// Stop stops dispatcher and closes all grpc streams.
func (d *Dispatcher) Stop() error {
	d.mu.Lock()
	if !d.isRunning() {
		return fmt.Errorf("dispatcher is already stopped")
	}
	d.cancel()
	d.mu.Unlock()
	d.nodes.Clean()
	// wait for all handlers to finish their raft deals, because manager will
	// set raftNode to nil
	d.wg.Wait()
	return nil
}

func (d *Dispatcher) addTask() error {
	d.mu.Lock()
	if !d.isRunning() {
		d.mu.Unlock()
		return grpc.Errorf(codes.Aborted, "dispatcher is stopped")
	}
	d.wg.Add(1)
	d.mu.Unlock()
	return nil
}

func (d *Dispatcher) doneTask() {
	d.wg.Done()
}

func (d *Dispatcher) markNodesUnknown(ctx context.Context) error {
	log := log.G(ctx).WithField("method", "(*Dispatcher).markNodesUnknown")
	var nodes []*api.Node
	var err error
	d.store.View(func(tx store.ReadTx) {
		nodes, err = store.FindNodes(tx, store.All)
	})
	if err != nil {
		return fmt.Errorf("failed to get list of nodes: %v", err)
	}
	_, err = d.store.Batch(func(batch *store.Batch) error {
		for _, n := range nodes {
			err := batch.Update(func(tx store.Tx) error {
				// check if node is still here
				node := store.GetNode(tx, n.ID)
				if node == nil {
					return nil
				}
				// do not try to resurrect down nodes
				if node.Status.State == api.NodeStatus_DOWN {
					return nil
				}
				node.Status = api.NodeStatus{
					State:   api.NodeStatus_UNKNOWN,
					Message: "Node marked as unknown due to leadership change in cluster",
				}
				nodeID := node.ID

				expireFunc := func() {
					log := log.WithField("node", nodeID)
					nodeStatus := api.NodeStatus{State: api.NodeStatus_DOWN, Message: "heartbeat failure for unknown node"}
					log.Debugf("heartbeat expiration for unknown node")
					if err := d.nodeRemove(nodeID, nodeStatus); err != nil {
						log.WithError(err).Errorf("failed deregistering node after heartbeat expiration for unknown node")
					}
				}
				if err := d.nodes.AddUnknown(node, expireFunc); err != nil {
					return fmt.Errorf("add unknown node failed: %v", err)
				}
				if err := store.UpdateNode(tx, node); err != nil {
					return fmt.Errorf("update failed %v", err)
				}
				return nil
			})
			if err != nil {
				log.WithField("node", n.ID).WithError(err).Errorf("failed to mark node as unknown")
			}
		}
		return nil
	})
	return err
}

func (d *Dispatcher) isRunning() bool {
	if d.ctx == nil {
		return false
	}
	select {
	case <-d.ctx.Done():
		return false
	default:
	}
	return true
}

// register is used for registration of node with particular dispatcher.
func (d *Dispatcher) register(ctx context.Context, nodeID string, description *api.NodeDescription) (string, string, error) {
	// prevent register until we're ready to accept it
	if err := d.addTask(); err != nil {
		return "", "", err
	}
	defer d.doneTask()

	// create or update node in store
	// TODO(stevvooe): Validate node specification.
	var node *api.Node
	// TODO(aaronl): Is it worth batching node creations?
	err := d.store.Update(func(tx store.Tx) error {
		node = store.GetNode(tx, nodeID)
		if node != nil {
			node.Description = description
			node.Status = api.NodeStatus{
				State: api.NodeStatus_READY,
			}
			return store.UpdateNode(tx, node)
		}

		node = &api.Node{
			ID:          nodeID,
			Description: description,
			Status: api.NodeStatus{
				State: api.NodeStatus_READY,
			},
		}
		return store.CreateNode(tx, node)
	})
	if err != nil {
		return "", "", err
	}

	expireFunc := func() {
		nodeStatus := api.NodeStatus{State: api.NodeStatus_DOWN, Message: "heartbeat failure"}
		log.G(ctx).Debugf("heartbeat expiration")
		if err := d.nodeRemove(nodeID, nodeStatus); err != nil {
			log.G(ctx).WithError(err).Errorf("failed deregistering node after heartbeat expiration")
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
	return rn.Node.ID, rn.SessionID, nil
}

// UpdateTaskStatus updates status of task. Node should send such updates
// on every status change of its tasks.
func (d *Dispatcher) UpdateTaskStatus(ctx context.Context, r *api.UpdateTaskStatusRequest) (*api.UpdateTaskStatusResponse, error) {
	nodeID, err := ca.AuthorizeNode(ctx)
	if err != nil {
		return nil, err
	}
	log := log.G(ctx).WithFields(logrus.Fields{
		"node.id":      nodeID,
		"node.session": r.SessionID,
		"method":       "(*Dispatcher).UpdateTaskStatus",
	})

	if err := d.addTask(); err != nil {
		return nil, err
	}
	defer d.doneTask()

	if _, err := d.nodes.GetWithSession(nodeID, r.SessionID); err != nil {
		return nil, err
	}
	d.taskUpdatesLock.Lock()
	for _, u := range r.Updates {
		if u.Status == nil {
			log.WithField("task.id", u.TaskID).Warnf("task report has nil status")
			continue
		}
		d.taskUpdates[u.TaskID] = u.Status
	}
	if len(d.taskUpdates) >= maxBatchItems {
		d.processTaskUpdatesTrigger <- struct{}{}
	}
	d.taskUpdatesLock.Unlock()
	return nil, nil
}

func (d *Dispatcher) processTaskUpdates() {
	d.taskUpdatesLock.Lock()
	if len(d.taskUpdates) == 0 {
		d.taskUpdatesLock.Unlock()
		return
	}
	taskUpdates := d.taskUpdates
	d.taskUpdates = make(map[string]*api.TaskStatus)
	d.taskUpdatesLock.Unlock()

	log := log.G(d.ctx).WithFields(logrus.Fields{
		"method": "(*Dispatcher).processTaskUpdates",
	})

	_, err := d.store.Batch(func(batch *store.Batch) error {
		for taskID, status := range taskUpdates {
			err := batch.Update(func(tx store.Tx) error {
				logger := log.WithField("task.id", taskID)
				task := store.GetTask(tx, taskID)
				if task == nil {
					logger.Errorf("task unavailable")
					return nil
				}

				logger = logger.WithField("state.transition", fmt.Sprintf("%v->%v", task.Status.State, status.State))

				if task.Status == *status {
					logger.Debug("task status identical, ignoring")
					return nil
				}

				if task.Status.State > status.State {
					logger.Debug("task status invalid transition")
					return nil
				}

				task.Status = *status
				if err := store.UpdateTask(tx, task); err != nil {
					logger.WithError(err).Error("failed to update task status")
					return nil
				}
				logger.Debug("task status updated")
				return nil
			})
			if err != nil {
				log.WithError(err).Error("dispatcher transaction failed")
			}
		}
		return nil
	})
	if err != nil {
		log.WithError(err).Error("dispatcher batch failed")
	}
}

// Tasks is a stream of tasks state for node. Each message contains full list
// of tasks which should be run on node, if task is not present in that list,
// it should be terminated.
func (d *Dispatcher) Tasks(r *api.TasksRequest, stream api.Dispatcher_TasksServer) error {
	nodeID, err := ca.AuthorizeNode(stream.Context())
	if err != nil {
		return err
	}

	if err := d.addTask(); err != nil {
		return err
	}
	defer d.doneTask()

	log.G(stream.Context()).WithFields(logrus.Fields{
		"node.id":      nodeID,
		"node.session": r.SessionID,
		"method":       "(*Dispatcher).Tasks",
	}).Debugf("")

	if _, err = d.nodes.GetWithSession(nodeID, r.SessionID); err != nil {
		return err
	}

	watchQueue := d.store.WatchQueue()
	nodeTasks, cancel := state.Watch(watchQueue,
		state.EventCreateTask{Task: &api.Task{NodeID: nodeID},
			Checks: []state.TaskCheckFunc{state.TaskCheckNodeID}},
		state.EventUpdateTask{Task: &api.Task{NodeID: nodeID},
			Checks: []state.TaskCheckFunc{state.TaskCheckNodeID}},
		state.EventDeleteTask{Task: &api.Task{NodeID: nodeID},
			Checks: []state.TaskCheckFunc{state.TaskCheckNodeID}})
	defer cancel()

	tasksMap := make(map[string]*api.Task)
	d.store.View(func(readTx store.ReadTx) {
		tasks, err := store.FindTasks(readTx, store.ByNodeID(nodeID))
		if err != nil {
			return
		}
		for _, t := range tasks {
			tasksMap[t.ID] = t
		}
	})

	for {
		if _, err := d.nodes.GetWithSession(nodeID, r.SessionID); err != nil {
			return err
		}

		var tasks []*api.Task
		for _, t := range tasksMap {
			// dispatcher only sends tasks that have been assigned to a node
			if t != nil && t.Status.State >= api.TaskStateAssigned {
				tasks = append(tasks, t)
			}
		}

		if err := stream.Send(&api.TasksMessage{Tasks: tasks}); err != nil {
			return err
		}

		select {
		case event := <-nodeTasks:
			switch v := event.(type) {
			case state.EventCreateTask:
				tasksMap[v.Task.ID] = v.Task
			case state.EventUpdateTask:
				tasksMap[v.Task.ID] = v.Task
			case state.EventDeleteTask:
				delete(tasksMap, v.Task.ID)
			}
		case <-stream.Context().Done():
			return stream.Context().Err()
		case <-d.ctx.Done():
			return d.ctx.Err()
		}
	}
}

func (d *Dispatcher) nodeRemove(id string, status api.NodeStatus) error {
	if err := d.addTask(); err != nil {
		return err
	}
	defer d.doneTask()
	// TODO(aaronl): Is it worth batching node removals?
	err := d.store.Update(func(tx store.Tx) error {
		node := store.GetNode(tx, id)
		if node == nil {
			return errors.New("node not found")
		}
		node.Status = status
		return store.UpdateNode(tx, node)
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
	nodeID, err := ca.AuthorizeNode(ctx)
	if err != nil {
		return nil, err
	}

	log.G(ctx).WithFields(logrus.Fields{
		"node.id":      nodeID,
		"node.session": r.SessionID,
		"method":       "(*Dispatcher).Heartbeat",
	}).Debugf("")

	period, err := d.nodes.Heartbeat(nodeID, r.SessionID)
	return &api.HeartbeatResponse{Period: period}, err
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
	ctx := stream.Context()
	nodeID, err := ca.AuthorizeNode(ctx)
	if err != nil {
		return err
	}

	if err := d.addTask(); err != nil {
		return err
	}
	defer d.doneTask()

	// register the node.
	nodeID, sessionID, err := d.register(stream.Context(), nodeID, r.Description)
	if err != nil {
		return err
	}

	log := log.G(ctx).WithFields(logrus.Fields{
		"node.id":      nodeID,
		"node.session": sessionID,
		"method":       "(*Dispatcher).Session",
	})
	if _, err = d.nodes.GetWithSession(nodeID, sessionID); err != nil {
		return err
	}

	if err := stream.Send(&api.SessionMessage{
		NodeID:     nodeID,
		SessionID:  sessionID,
		Managers:   d.getManagers(),
		Disconnect: false,
	}); err != nil {
		return err
	}

	mgrUpdates, mgrCancel := d.mgrQueue.Watch()
	defer mgrCancel()

	for {
		// After each message send, we need to check the nodes sessionID hasn't
		// changed. If it has, we will the stream and make the node
		// re-register.
		node, err := d.nodes.GetWithSession(nodeID, sessionID)
		if err != nil {
			return err
		}
		var (
			disconnectError error
			mgrs            []*api.WeightedPeer
		)
		select {
		case ev := <-mgrUpdates:
			mgrs = ev.([]*api.WeightedPeer)
		case <-stream.Context().Done():
			return stream.Context().Err()
		case <-node.Disconnect:
			disconnectError = grpc.Errorf(codes.Aborted, "node must disconnect")
		case <-d.ctx.Done():
			disconnectError = grpc.Errorf(codes.Aborted, "dispatcher stopped")
		}
		if mgrs == nil {
			mgrs = d.getManagers()
		}
		if disconnectError != nil {
			nodeStatus := api.NodeStatus{State: api.NodeStatus_DISCONNECTED, Message: "node is currently trying to find new manager"}
			if err := d.nodeRemove(nodeID, nodeStatus); err != nil {
				log.WithError(err).Error("failed to remove node")
			}
		}

		if err := stream.Send(&api.SessionMessage{
			Managers:   mgrs,
			Disconnect: disconnectError != nil,
		}); err != nil {
			return err
		}
		if disconnectError != nil {
			log.WithError(disconnectError).Error("session end")
			return disconnectError
		}
	}
}

// NodeCount returns number of nodes which connected to this dispatcher.
func (d *Dispatcher) NodeCount() int {
	return d.nodes.Len()
}
