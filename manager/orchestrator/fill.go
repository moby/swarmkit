package orchestrator

import (
	"reflect"

	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/identity"
	"github.com/docker/swarm-v2/log"
	"github.com/docker/swarm-v2/manager/state"
	"golang.org/x/net/context"
)

// An FillOrchestrator runs a reconciliation loop to create and destroy
// tasks as necessary for fill services.
type FillOrchestrator struct {
	store state.WatchableStore
	// nodes contains nodeID of all valid nodes in the cluster
	nodes map[string]struct{}
	// fillServices have all the FILL services in the cluster, indexed by ServiceID
	fillServices map[string]*api.Service

	// stopChan signals to the state machine to stop running.
	stopChan chan struct{}
	// doneChan is closed when the state machine terminates.
	doneChan chan struct{}
}

// NewFillOrchestrator creates a new FillOrchestrator
func NewFillOrchestrator(store state.WatchableStore) *FillOrchestrator {
	return &FillOrchestrator{
		store:        store,
		nodes:        make(map[string]struct{}),
		fillServices: make(map[string]*api.Service),
		stopChan:     make(chan struct{}),
		doneChan:     make(chan struct{}),
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
	var nodes []*api.Node
	err := f.store.View(func(readTx state.ReadTx) error {
		var err error
		nodes, err = readTx.Nodes().Find(state.All)
		return err
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
	err = f.store.View(func(readTx state.ReadTx) error {
		var err error
		existingServices, err = readTx.Services().Find(state.ByServiceMode(api.ServiceModeFill))
		return err
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
				if !f.isRelatedService(v.Service) {
					continue
				}
				f.fillServices[v.Service.ID] = v.Service
				f.reconcileOneService(ctx, v.Service)
			case state.EventUpdateService:
				if !f.isRelatedService(v.Service) {
					continue
				}
				f.fillServices[v.Service.ID] = v.Service
				f.reconcileOneService(ctx, v.Service)
			case state.EventDeleteService:
				if !f.isRelatedService(v.Service) {
					continue
				}
				f.deleteService(ctx, v.Service)
			case state.EventCreateNode:
				f.nodes[v.Node.ID] = struct{}{}
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
				f.deleteNode(ctx, v.Node)
			case state.EventUpdateTask:
				if _, exists := f.fillServices[v.Task.ServiceID]; !exists {
					continue
				}
				// fill orchestrator needs to inspect when a task has terminated
				// it should ignore tasks whose DesiredState is dead, which means the
				// task has been processed
				if isTaskTerminated(v.Task) && v.Task.DesiredState != api.TaskStateDead {
					f.reconcileServiceOneNode(ctx, v.Task.ServiceID, v.Task.NodeID)
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
}

func (f *FillOrchestrator) deleteService(ctx context.Context, service *api.Service) {
	var tasks []*api.Task
	err := f.store.View(func(tx state.ReadTx) error {
		var err error
		tasks, err = tx.Tasks().Find(state.ByServiceID(service.ID))
		return err
	})
	if err != nil {
		log.G(ctx).WithError(err).Errorf("deleteService transaction failed")
		return
	}

	_, err = f.store.Batch(func(batch state.Batch) error {
		for _, t := range tasks {
			err := batch.Update(func(tx state.Tx) error {
				if t != nil {
					t.DesiredState = api.TaskStateDead
					return tx.Tasks().Update(t)
				}
				return nil
			})
			if err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		log.G(ctx).WithError(err).Errorf("deleteService transaction failed")
	}
	// delete the service from service map
	delete(f.fillServices, service.ID)
}

func (f *FillOrchestrator) removeTasksFromNode(ctx context.Context, node *api.Node) {
	var tasks []*api.Task
	err := f.store.Update(func(tx state.Tx) error {
		var err error
		tasks, err = tx.Tasks().Find(state.ByNodeID(node.ID))
		return err
	})
	if err != nil {
		log.G(ctx).WithError(err).Errorf("fillOrchestrator: deleteNode failed finding tasks")
		return
	}

	_, err = f.store.Batch(func(batch state.Batch) error {
		for _, t := range tasks {
			// fillOrchestrator only removes tasks from fillServices
			if _, exists := f.fillServices[t.ServiceID]; exists {
				err := batch.Update(func(tx state.Tx) error {
					f.removeTask(ctx, tx, t)
					return nil
				})
				if err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		log.G(ctx).WithError(err).Errorf("fillOrchestrator: removeTasksFromNode failed")
	}
}

func (f *FillOrchestrator) deleteNode(ctx context.Context, node *api.Node) {
	f.removeTasksFromNode(ctx, node)
	// remove the node from node list
	delete(f.nodes, node.ID)
}

func (f *FillOrchestrator) reconcileOneService(ctx context.Context, service *api.Service) {
	var tasks []*api.Task
	err := f.store.Update(func(tx state.Tx) error {
		var err error
		tasks, err = tx.Tasks().Find(state.ByServiceID(service.ID))
		return err
	})
	if err != nil {
		log.G(ctx).WithError(err).Errorf("fillOrchestrator: reconcileOneService failed finding tasks")
		return
	}
	restartPolicy := restartCondition(service)
	// a node may have completed this servie
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

	_, err = f.store.Batch(func(batch state.Batch) error {
		for nodeID := range f.nodes {
			err := batch.Update(func(tx state.Tx) error {
				ntasks := nodeTasks[nodeID]
				// if restart policy considers this node has finished its task
				// it should remove all running tasks
				if _, exists := nodeCompleted[nodeID]; exists {
					f.removeTasks(ctx, tx, service, ntasks)
					return nil
				}
				// this node needs to run 1 copy of the task
				if len(ntasks) == 0 {
					f.addTask(ctx, tx, service, nodeID)
				} else {
					f.updateTask(ctx, tx, service, ntasks[0])
					f.removeTasks(ctx, tx, service, ntasks[1:])
				}
				return nil
			})
			if err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		log.G(ctx).WithError(err).Errorf("FillOrchestrator: reconcileOneService transaction failed")
	}
}

// reconcileOneNode checks all fill services on one node
func (f *FillOrchestrator) reconcileOneNode(ctx context.Context, node *api.Node) {
	if _, exists := f.nodes[node.ID]; !exists {
		log.G(ctx).Debugf("fillOrchestrator: node %s not in current node list", node.ID)
		return
	}
	if isNodeInDrainState(node) {
		log.G(ctx).Debugf("fillOrchestrator: node %s in drain state, remove tasks from it", node.ID)
		f.deleteNode(ctx, node)
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
	var tasks []*api.Task
	err := f.store.Update(func(tx state.Tx) error {
		var err error
		tasksOnNode, err := tx.Tasks().Find(state.ByNodeID(nodeID))
		if err != nil {
			log.G(ctx).WithError(err).Errorf("fillOrchestrator: reconcile failed finding tasks")
			return err
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

		// if restart policy considers this node has finished its task
		// it should remove all running tasks
		if completed {
			f.removeTasks(ctx, tx, service, tasks)
			return nil
		}
		// this node needs to run 1 copy of the task
		if len(tasks) == 0 {
			f.addTask(ctx, tx, service, nodeID)
		} else {
			f.updateTask(ctx, tx, service, tasks[0])
			f.removeTasks(ctx, tx, service, tasks[1:])
		}
		return nil
	})
	if err != nil {
		log.G(ctx).WithError(err).Errorf("FillOrchestrator: reconcileServiceOneNode transaction failed")
	}
}

func (f *FillOrchestrator) updateTask(ctx context.Context, tx state.Tx, service *api.Service, t *api.Task) {
	if t == nil || service == nil {
		return
	}
	if reflect.DeepEqual(service.Spec.Template, t.Spec) {
		return
	}
	f.addTask(ctx, tx, service, t.NodeID)
	f.removeTask(ctx, tx, t)
}

func (f *FillOrchestrator) removeTask(ctx context.Context, tx state.Tx, t *api.Task) {
	// set existing task DesiredState to TaskStateDead
	// TODO(aaronl): optimistic update?
	t = tx.Tasks().Get(t.ID)
	if t != nil {
		t.DesiredState = api.TaskStateDead
		err := tx.Tasks().Update(t)
		if err != nil {
			log.G(ctx).Errorf("FillOrchestrator: removeTask failed to remove %s: %v", t.ID, err)
		}
	}
}

func (f *FillOrchestrator) addTask(ctx context.Context, tx state.Tx, service *api.Service, nodeID string) {
	spec := *service.Spec.Template
	annotations := service.Spec.Annotations

	task := &api.Task{
		ID:          identity.NewID(),
		Annotations: annotations,
		Spec:        &spec,
		ServiceID:   service.ID,
		NodeID:      nodeID,
		Status: &api.TaskStatus{
			State: api.TaskStateNew,
		},
		DesiredState: api.TaskStateRunning,
	}
	if err := tx.Tasks().Create(task); err != nil {
		log.G(ctx).Errorf("FillOrchestrator: failed to create task: %v", err)
	}
}

func (f *FillOrchestrator) removeTasks(ctx context.Context, tx state.Tx, service *api.Service, tasks []*api.Task) {
	for _, t := range tasks {
		f.removeTask(ctx, tx, t)
	}
}

func (f *FillOrchestrator) isRelatedService(service *api.Service) bool {
	return service != nil && service.Spec != nil && service.Spec.Mode == api.ServiceModeFill
}

func isTaskRunning(t *api.Task) bool {
	return t != nil && t.DesiredState == api.TaskStateRunning && t.Status != nil && t.Status.State <= api.TaskStateRunning
}

func isValidNode(n *api.Node) bool {
	// current simulation spec could be nil
	return n != nil &&
		(n.Spec == nil || n.Spec != nil && n.Spec.Availability != api.NodeAvailabilityDrain)
}

func isNodeInDrainState(n *api.Node) bool {
	return n != nil && n.Spec != nil && n.Spec.Availability == api.NodeAvailabilityDrain
}

func isTaskCompleted(t *api.Task, restartPolicy api.RestartPolicy_RestartCondition) bool {
	if t == nil || isTaskRunning(t) || t.Status == nil {
		return false
	}
	return restartPolicy == api.RestartNever ||
		(restartPolicy == api.RestartOnFailure && t.Status.TerminalState == api.TaskStateCompleted)
}

func isTaskTerminated(t *api.Task) bool {
	return t != nil && t.Status != nil && t.Status.TerminalState > api.TaskStateNew
}
