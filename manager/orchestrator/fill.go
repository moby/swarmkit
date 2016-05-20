package orchestrator

import (
	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/log"
	"github.com/docker/swarm-v2/manager/state"
	"github.com/docker/swarm-v2/manager/state/store"
	"golang.org/x/net/context"
)

// An FillOrchestrator runs a reconciliation loop to create and destroy
// tasks as necessary for fill services.
type FillOrchestrator struct {
	store *store.MemoryStore
	// nodes contains nodeID of all valid nodes in the cluster
	nodes map[string]struct{}
	// fillServices have all the FILL services in the cluster, indexed by ServiceID
	fillServices map[string]*api.Service

	// stopChan signals to the state machine to stop running.
	stopChan chan struct{}
	// doneChan is closed when the state machine terminates.
	doneChan chan struct{}

	updater  *UpdateSupervisor
	restarts *RestartSupervisor
}

// NewFillOrchestrator creates a new FillOrchestrator
func NewFillOrchestrator(store *store.MemoryStore) *FillOrchestrator {
	return &FillOrchestrator{
		store:        store,
		nodes:        make(map[string]struct{}),
		fillServices: make(map[string]*api.Service),
		stopChan:     make(chan struct{}),
		doneChan:     make(chan struct{}),
		updater:      NewUpdateSupervisor(store),
		restarts:     NewRestartSupervisor(store),
	}
}

// Run contains the FillOrchestrator event loop
func (f *FillOrchestrator) Run(ctx context.Context) error {
	defer close(f.doneChan)

	// Watch changes to services and tasks
	queue := f.store.WatchQueue()
	watcher, cancel := queue.Watch()
	defer cancel()

	// Get list of nodes
	var (
		nodes []*api.Node
		err   error
	)
	f.store.View(func(readTx store.ReadTx) {
		nodes, err = store.FindNodes(readTx, store.All)
	})
	if err != nil {
		return err
	}
	for _, n := range nodes {
		// if a node is in drain state, do not add it
		if isValidNode(n) {
			f.nodes[n.ID] = struct{}{}
		}
	}

	// Lookup existing fill services
	var existingServices []*api.Service
	f.store.View(func(readTx store.ReadTx) {
		existingServices, err = store.FindServices(readTx, store.ByServiceMode(api.ServiceModeFill))
	})
	if err != nil {
		return err
	}
	for _, s := range existingServices {
		f.fillServices[s.ID] = s
		f.reconcileOneService(ctx, s)
	}

	for {
		select {
		case event := <-watcher:
			// TODO(stevvooe): Use ctx to limit running time of operation.
			switch v := event.(type) {
			case state.EventCreateService:
				if v.Service.Spec.Mode != api.ServiceModeFill {
					continue
				}
				f.fillServices[v.Service.ID] = v.Service
				f.reconcileOneService(ctx, v.Service)
			case state.EventUpdateService:
				if v.Service.Spec.Mode != api.ServiceModeFill {
					continue
				}
				f.fillServices[v.Service.ID] = v.Service
				f.reconcileOneService(ctx, v.Service)
			case state.EventDeleteService:
				if v.Service.Spec.Mode != api.ServiceModeFill {
					continue
				}
				deleteServiceTasks(ctx, f.store, v.Service)
				// delete the service from service map
				delete(f.fillServices, v.Service.ID)
				f.restarts.ClearServiceHistory(v.Service.ID)
			case state.EventCreateNode:
				f.reconcileOneNode(ctx, v.Node)
			case state.EventUpdateNode:
				switch v.Node.Status.State {
				// NodeStatus_DISCONNECTED is a transient state, no need to make any change
				case api.NodeStatus_DOWN:
					f.removeTasksFromNode(ctx, v.Node)
				case api.NodeStatus_READY:
					// node could come back to READY from DOWN or DISCONNECT
					f.reconcileOneNode(ctx, v.Node)
				}
			case state.EventDeleteNode:
				f.removeTasksFromNode(ctx, v.Node)
				delete(f.nodes, v.Node.ID)
			case state.EventUpdateTask:
				if _, exists := f.fillServices[v.Task.ServiceID]; !exists {
					continue
				}
				// fill orchestrator needs to inspect when a task has terminated
				// it should ignore tasks whose DesiredState is past running, which
				// means the task has been processed
				if isTaskTerminated(v.Task) {
					f.restartTask(ctx, v.Task.ID, v.Task.ServiceID)
				}
			case state.EventDeleteTask:
				// CLI allows deleting task
				if _, exists := f.fillServices[v.Task.ServiceID]; !exists {
					continue
				}
				f.reconcileServiceOneNode(ctx, v.Task.ServiceID, v.Task.NodeID)
			}
		case <-f.stopChan:
			return nil
		}
	}
}

// Stop stops the orchestrator.
func (f *FillOrchestrator) Stop() {
	close(f.stopChan)
	<-f.doneChan
	f.updater.CancelAll()
	f.restarts.CancelAll()
}

func (f *FillOrchestrator) removeTasksFromNode(ctx context.Context, node *api.Node) {
	var (
		tasks []*api.Task
		err   error
	)
	f.store.View(func(tx store.ReadTx) {
		tasks, err = store.FindTasks(tx, store.ByNodeID(node.ID))
	})
	if err != nil {
		log.G(ctx).WithError(err).Errorf("fillOrchestrator: removeTasksFromNode failed finding tasks")
		return
	}

	_, err = f.store.Batch(func(batch *store.Batch) error {
		for _, t := range tasks {
			// fillOrchestrator only removes tasks from fillServices
			if _, exists := f.fillServices[t.ServiceID]; exists {
				f.removeTask(ctx, batch, t)
			}
		}
		return nil
	})
	if err != nil {
		log.G(ctx).WithError(err).Errorf("fillOrchestrator: removeTasksFromNode failed")
	}
}

func (f *FillOrchestrator) reconcileOneService(ctx context.Context, service *api.Service) {
	var (
		tasks []*api.Task
		err   error
	)
	f.store.View(func(tx store.ReadTx) {
		tasks, err = store.FindTasks(tx, store.ByServiceID(service.ID))
	})
	if err != nil {
		log.G(ctx).WithError(err).Errorf("fillOrchestrator: reconcileOneService failed finding tasks")
		return
	}
	restartPolicy := restartCondition(service)
	// a node may have completed this service
	nodeCompleted := make(map[string]struct{})
	// nodeID -> task list
	nodeTasks := make(map[string][]*api.Task)

	for _, t := range tasks {
		if isTaskRunning(t) {
			// Collect all running instances of this service
			nodeTasks[t.NodeID] = append(nodeTasks[t.NodeID], t)
		} else {
			// for finished tasks, check restartPolicy
			if isTaskCompleted(t, restartPolicy) {
				nodeCompleted[t.NodeID] = struct{}{}
			}
		}
	}

	_, err = f.store.Batch(func(batch *store.Batch) error {
		var updateTasks []*api.Task
		for nodeID := range f.nodes {
			ntasks := nodeTasks[nodeID]
			// if restart policy considers this node has finished its task
			// it should remove all running tasks
			if _, exists := nodeCompleted[nodeID]; exists {
				f.removeTasks(ctx, batch, service, ntasks)
				return nil
			}
			// this node needs to run 1 copy of the task
			if len(ntasks) == 0 {
				f.addTask(ctx, batch, service, nodeID)
			} else {
				updateTasks = append(updateTasks, ntasks[0])
				f.removeTasks(ctx, batch, service, ntasks[1:])
			}
		}
		if len(updateTasks) > 0 {
			f.updater.Update(ctx, service, updateTasks)
		}
		return nil
	})
	if err != nil {
		log.G(ctx).WithError(err).Errorf("FillOrchestrator: reconcileOneService transaction failed")
	}
}

// reconcileOneNode checks all fill services on one node
func (f *FillOrchestrator) reconcileOneNode(ctx context.Context, node *api.Node) {
	switch node.Spec.Availability {
	case api.NodeAvailabilityDrain:
		log.G(ctx).Debugf("fillOrchestrator: node %s in drain state, removing tasks from it", node.ID)
		f.removeTasksFromNode(ctx, node)
		delete(f.nodes, node.ID)
		return
	case api.NodeAvailabilityActive:
		if _, exists := f.nodes[node.ID]; !exists {
			log.G(ctx).Debugf("fillOrchestrator: node %s not in current node list, adding it", node.ID)
			f.nodes[node.ID] = struct{}{}
		}
	default:
		log.G(ctx).Debugf("fillOrchestrator: node %s in %s state, doing nothing", node.ID, node.Spec.Availability.String())
		return
	}
	// typically there are only a few fill services on a node
	// iterate thru all of them one by one. If raft store visits become a concern,
	// it can be optimized.
	for _, service := range f.fillServices {
		f.reconcileServiceOneNode(ctx, service.ID, node.ID)
	}
}

// reconcileServiceOneNode checks one service on one node
func (f *FillOrchestrator) reconcileServiceOneNode(ctx context.Context, serviceID string, nodeID string) {
	_, exists := f.nodes[nodeID]
	if !exists {
		return
	}
	service, exists := f.fillServices[serviceID]
	if !exists {
		return
	}
	restartPolicy := restartCondition(service)
	// the node has completed this servie
	completed := false
	// tasks for this node and service
	var (
		tasks []*api.Task
		err   error
	)
	f.store.View(func(tx store.ReadTx) {
		var tasksOnNode []*api.Task
		tasksOnNode, err = store.FindTasks(tx, store.ByNodeID(nodeID))
		if err != nil {
			return
		}
		for _, t := range tasksOnNode {
			// only interested in one service
			if t.ServiceID != serviceID {
				continue
			}
			if isTaskRunning(t) {
				tasks = append(tasks, t)
			} else {
				if isTaskCompleted(t, restartPolicy) {
					completed = true
				}
			}
		}
	})
	if err != nil {
		log.G(ctx).WithError(err).Errorf("fillOrchestrator: reconcile failed finding tasks")
		return
	}

	_, err = f.store.Batch(func(batch *store.Batch) error {
		// if restart policy considers this node has finished its task
		// it should remove all running tasks
		if completed {
			f.removeTasks(ctx, batch, service, tasks)
			return nil
		}
		// this node needs to run 1 copy of the task
		if len(tasks) == 0 {
			f.addTask(ctx, batch, service, nodeID)
		} else {
			f.removeTasks(ctx, batch, service, tasks[1:])
		}
		return nil
	})
	if err != nil {
		log.G(ctx).WithError(err).Errorf("FillOrchestrator: reconcileServiceOneNode batch failed")
	}
}

// restartTask calls the restart supervisor's Restart function, which
// sets a task's desired state to dead and restarts it if the restart
// policy calls for it to be restarted.
func (f *FillOrchestrator) restartTask(ctx context.Context, taskID string, serviceID string) {
	err := f.store.Update(func(tx store.Tx) error {
		t := store.GetTask(tx, taskID)
		if t == nil || t.DesiredState > api.TaskStateRunning {
			return nil
		}
		service := store.GetService(tx, serviceID)
		if service == nil {
			return nil
		}
		return f.restarts.Restart(ctx, tx, service, *t)
	})
	if err != nil {
		log.G(ctx).WithError(err).Errorf("FillOrchestrator: restartTask transaction failed")
	}
}

func (f *FillOrchestrator) removeTask(ctx context.Context, batch *store.Batch, t *api.Task) {
	// set existing task DesiredState to TaskStateShutdown
	// TODO(aaronl): optimistic update?
	err := batch.Update(func(tx store.Tx) error {
		t = store.GetTask(tx, t.ID)
		if t != nil {
			t.DesiredState = api.TaskStateShutdown
			return store.UpdateTask(tx, t)
		}
		return nil
	})
	if err != nil {
		log.G(ctx).WithError(err).Errorf("FillOrchestrator: removeTask failed to remove %s", t.ID)
	}
}

func (f *FillOrchestrator) addTask(ctx context.Context, batch *store.Batch, service *api.Service, nodeID string) {
	task := newTask(service, 0)
	task.NodeID = nodeID

	err := batch.Update(func(tx store.Tx) error {
		return store.CreateTask(tx, task)
	})
	if err != nil {
		log.G(ctx).WithError(err).Errorf("FillOrchestrator: failed to create task")
	}
}

func (f *FillOrchestrator) removeTasks(ctx context.Context, batch *store.Batch, service *api.Service, tasks []*api.Task) {
	for _, t := range tasks {
		f.removeTask(ctx, batch, t)
	}
}

func isTaskRunning(t *api.Task) bool {
	return t != nil && t.DesiredState <= api.TaskStateRunning && t.Status.State <= api.TaskStateRunning
}

func isValidNode(n *api.Node) bool {
	// current simulation spec could be nil
	return n != nil && n.Spec.Availability != api.NodeAvailabilityDrain
}

func isTaskCompleted(t *api.Task, restartPolicy api.RestartPolicy_RestartCondition) bool {
	if t == nil || isTaskRunning(t) {
		return false
	}
	return restartPolicy == api.RestartOnNone ||
		(restartPolicy == api.RestartOnFailure && t.Status.TerminalState == api.TaskStateCompleted)
}

func isTaskTerminated(t *api.Task) bool {
	return t != nil && t.Status.TerminalState > api.TaskStateNew
}
