package orchestrator

import (
	"github.com/docker/swarmkit/api"
	"github.com/docker/swarmkit/log"
	"github.com/docker/swarmkit/manager/constraint"
	"github.com/docker/swarmkit/manager/state"
	"github.com/docker/swarmkit/manager/state/store"
	"golang.org/x/net/context"
)

type globalService struct {
	*api.Service

	// Compiled constraints
	constraints []constraint.Constraint
}

// GlobalOrchestrator runs a reconciliation loop to create and destroy
// tasks as necessary for global services.
type GlobalOrchestrator struct {
	store *store.MemoryStore
	// nodes is the set of non-drained nodes in the cluster, indexed by node ID
	nodes map[string]*api.Node
	// globalServices has all the global services in the cluster, indexed by ServiceID
	globalServices map[string]globalService

	// stopChan signals to the state machine to stop running.
	stopChan chan struct{}
	// doneChan is closed when the state machine terminates.
	doneChan chan struct{}

	updater  *UpdateSupervisor
	restarts *RestartSupervisor

	cluster *api.Cluster // local instance of the cluster
}

// NewGlobalOrchestrator creates a new GlobalOrchestrator
func NewGlobalOrchestrator(store *store.MemoryStore) *GlobalOrchestrator {
	restartSupervisor := NewRestartSupervisor(store)
	updater := NewUpdateSupervisor(store, restartSupervisor)
	return &GlobalOrchestrator{
		store:          store,
		nodes:          make(map[string]*api.Node),
		globalServices: make(map[string]globalService),
		stopChan:       make(chan struct{}),
		doneChan:       make(chan struct{}),
		updater:        updater,
		restarts:       restartSupervisor,
	}
}

// Run contains the GlobalOrchestrator event loop
func (g *GlobalOrchestrator) Run(ctx context.Context) error {
	defer close(g.doneChan)

	// Watch changes to services and tasks
	queue := g.store.WatchQueue()
	watcher, cancel := queue.Watch()
	defer cancel()

	// lookup the cluster
	var err error
	g.store.View(func(readTx store.ReadTx) {
		var clusters []*api.Cluster
		clusters, err = store.FindClusters(readTx, store.ByName("default"))

		if len(clusters) != 1 {
			return // just pick up the cluster when it is created.
		}
		g.cluster = clusters[0]
	})
	if err != nil {
		return err
	}

	// Get list of nodes
	var nodes []*api.Node
	g.store.View(func(readTx store.ReadTx) {
		nodes, err = store.FindNodes(readTx, store.All)
	})
	if err != nil {
		return err
	}
	for _, n := range nodes {
		g.updateNode(n)
	}

	// Lookup global services
	var existingServices []*api.Service
	g.store.View(func(readTx store.ReadTx) {
		existingServices, err = store.FindServices(readTx, store.All)
	})
	if err != nil {
		return err
	}
	for _, s := range existingServices {
		if isGlobalService(s) {
			g.updateService(s)
			g.reconcileOneService(ctx, s.ID)
		}
	}

	for {
		select {
		case event := <-watcher:
			// TODO(stevvooe): Use ctx to limit running time of operation.
			switch v := event.(type) {
			case state.EventUpdateCluster:
				g.cluster = v.Cluster
			case state.EventCreateService:
				if !isGlobalService(v.Service) {
					continue
				}
				g.updateService(v.Service)
				g.reconcileOneService(ctx, v.Service.ID)
			case state.EventUpdateService:
				if !isGlobalService(v.Service) {
					continue
				}
				g.updateService(v.Service)
				g.reconcileOneService(ctx, v.Service.ID)
			case state.EventDeleteService:
				if !isGlobalService(v.Service) {
					continue
				}
				deleteServiceTasks(ctx, g.store, v.Service)
				// delete the service from service map
				delete(g.globalServices, v.Service.ID)
				g.restarts.ClearServiceHistory(v.Service.ID)
			case state.EventCreateNode:
				g.updateNode(v.Node)
				g.reconcileOneNode(ctx, v.Node)
			case state.EventUpdateNode:
				g.updateNode(v.Node)
				switch v.Node.Status.State {
				// NodeStatus_DISCONNECTED is a transient state, no need to make any change
				case api.NodeStatus_DOWN:
					g.removeTasksFromNode(ctx, v.Node)
				case api.NodeStatus_READY:
					// node could come back to READY from DOWN or DISCONNECT
					g.reconcileOneNode(ctx, v.Node)
				}
			case state.EventDeleteNode:
				g.removeTasksFromNode(ctx, v.Node)
				delete(g.nodes, v.Node.ID)
			case state.EventUpdateTask:
				if _, exists := g.globalServices[v.Task.ServiceID]; !exists {
					continue
				}
				// global orchestrator needs to inspect when a task has terminated
				// it should ignore tasks whose DesiredState is past running, which
				// means the task has been processed
				if isTaskTerminated(v.Task) {
					g.restartTask(ctx, v.Task.ID, v.Task.ServiceID)
				}
			case state.EventDeleteTask:
				// CLI allows deleting task
				if _, exists := g.globalServices[v.Task.ServiceID]; !exists {
					continue
				}
				g.reconcileServiceOneNode(ctx, v.Task.ServiceID, v.Task.NodeID)
			}
		case <-g.stopChan:
			return nil
		}
	}
}

// Stop stops the orchestrator.
func (g *GlobalOrchestrator) Stop() {
	close(g.stopChan)
	<-g.doneChan
	g.updater.CancelAll()
	g.restarts.CancelAll()
}

func (g *GlobalOrchestrator) removeTasksFromNode(ctx context.Context, node *api.Node) {
	var (
		tasks []*api.Task
		err   error
	)
	g.store.View(func(tx store.ReadTx) {
		tasks, err = store.FindTasks(tx, store.ByNodeID(node.ID))
	})
	if err != nil {
		log.G(ctx).WithError(err).Errorf("global orchestrator: removeTasksFromNode failed finding tasks")
		return
	}

	_, err = g.store.Batch(func(batch *store.Batch) error {
		for _, t := range tasks {
			// GlobalOrchestrator only removes tasks from globalServices
			if _, exists := g.globalServices[t.ServiceID]; exists {
				g.removeTask(ctx, batch, t)
			}
		}
		return nil
	})
	if err != nil {
		log.G(ctx).WithError(err).Errorf("global orchestrator: removeTasksFromNode failed")
	}
}

func (g *GlobalOrchestrator) reconcileOneService(ctx context.Context, serviceID string) {
	service, exists := g.globalServices[serviceID]
	if !exists {
		return
	}

	var (
		tasks []*api.Task
		err   error
	)
	g.store.View(func(tx store.ReadTx) {
		tasks, err = store.FindTasks(tx, store.ByServiceID(service.ID))
	})
	if err != nil {
		log.G(ctx).WithError(err).Errorf("global orchestrator: reconcileOneService failed finding tasks")
		return
	}
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
			if isTaskCompleted(t, restartCondition(t)) {
				nodeCompleted[t.NodeID] = struct{}{}
			}
		}
	}

	_, err = g.store.Batch(func(batch *store.Batch) error {
		var updateTasks []slot
		for nodeID, node := range g.nodes {
			meetsConstraints := constraint.NodeMatches(service.constraints, node)
			ntasks := nodeTasks[nodeID]
			delete(nodeTasks, nodeID)

			// if restart policy considers this node has finished its task
			// it should remove all running tasks
			if _, exists := nodeCompleted[nodeID]; exists || !meetsConstraints {
				g.removeTasks(ctx, batch, ntasks)
				continue
			}

			if node.Spec.Availability == api.NodeAvailabilityPause {
				// the node is paused, so we won't add or update
				// any tasks
				continue
			}

			// this node needs to run 1 copy of the task
			if len(ntasks) == 0 {
				g.addTask(ctx, batch, service.Service, nodeID)
			} else {
				updateTasks = append(updateTasks, ntasks)
			}
		}
		if len(updateTasks) > 0 {
			g.updater.Update(ctx, g.cluster, service.Service, updateTasks)
		}

		// Remove any tasks assigned to nodes not found in g.nodes.
		// These must be associated with nodes that are drained, or
		// nodes that no longer exist.
		for _, ntasks := range nodeTasks {
			g.removeTasks(ctx, batch, ntasks)
		}
		return nil
	})
	if err != nil {
		log.G(ctx).WithError(err).Errorf("global orchestrator: reconcileOneService transaction failed")
	}
}

// updateNode updates g.nodes based on the current node value
func (g *GlobalOrchestrator) updateNode(node *api.Node) {
	if node.Spec.Availability == api.NodeAvailabilityDrain {
		delete(g.nodes, node.ID)
	} else {
		g.nodes[node.ID] = node
	}
}

// updateService updates g.globalServices based on the current service value
func (g *GlobalOrchestrator) updateService(service *api.Service) {
	var constraints []constraint.Constraint

	if service.Spec.Task.Placement != nil && len(service.Spec.Task.Placement.Constraints) != 0 {
		constraints, _ = constraint.Parse(service.Spec.Task.Placement.Constraints)
	}

	g.globalServices[service.ID] = globalService{
		Service:     service,
		constraints: constraints,
	}
}

// reconcileOneNode checks all global services on one node
func (g *GlobalOrchestrator) reconcileOneNode(ctx context.Context, node *api.Node) {
	if node.Spec.Availability == api.NodeAvailabilityDrain {
		log.G(ctx).Debugf("global orchestrator: node %s in drain state, removing tasks from it", node.ID)
		g.removeTasksFromNode(ctx, node)
		return
	}

	// typically there are only a few global services on a node
	// iterate through all of them one by one. If raft store visits become a concern,
	// it can be optimized.
	for _, service := range g.globalServices {
		g.reconcileServiceOneNode(ctx, service.ID, node.ID)
	}
}

// reconcileServiceOneNode checks one service on one node
func (g *GlobalOrchestrator) reconcileServiceOneNode(ctx context.Context, serviceID string, nodeID string) {
	node, exists := g.nodes[nodeID]
	if !exists {
		return
	}
	service, exists := g.globalServices[serviceID]
	if !exists {
		return
	}
	// the node has completed this servie
	completed := false
	// tasks for this node and service
	var (
		tasks []*api.Task
		err   error
	)
	g.store.View(func(tx store.ReadTx) {
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
				if isTaskCompleted(t, restartCondition(t)) {
					completed = true
				}
			}
		}
	})
	if err != nil {
		log.G(ctx).WithError(err).Errorf("global orchestrator: reconcile failed finding tasks")
		return
	}

	_, err = g.store.Batch(func(batch *store.Batch) error {
		meetsConstraints := constraint.NodeMatches(service.constraints, node)

		// if restart policy considers this node has finished its task
		// it should remove all running tasks
		if completed || !meetsConstraints {
			g.removeTasks(ctx, batch, tasks)
			return nil
		}

		if node.Spec.Availability == api.NodeAvailabilityPause {
			// the node is paused, so we won't add or update tasks
			return nil
		}

		if len(tasks) == 0 {
			g.addTask(ctx, batch, service.Service, nodeID)
		} else {
			// If task is out of date, update it. This can happen
			// on node reconciliation if, for example, we drain a
			// node, update the service, and then activate the node
			// later.

			// We don't use g.updater here for two reasons:
			// - This is not a rolling update. Since it was not
			//   triggered directly by updating the service, it
			//   should not observe the rolling update parameters
			//   or show status in UpdateStatus.
			// - Calling Update cancels any current rolling updates
			//   for the service, such as one triggered by service
			//   reconciliation.

			var (
				dirtyTasks []*api.Task
				cleanTasks []*api.Task
			)

			for _, t := range tasks {
				if isTaskDirty(service.Service, t) {
					dirtyTasks = append(dirtyTasks, t)
				} else {
					cleanTasks = append(cleanTasks, t)
				}
			}

			if len(cleanTasks) == 0 {
				g.addTask(ctx, batch, service.Service, nodeID)
			} else {
				dirtyTasks = append(dirtyTasks, cleanTasks[1:]...)
			}
			g.removeTasks(ctx, batch, dirtyTasks)
		}
		return nil
	})
	if err != nil {
		log.G(ctx).WithError(err).Errorf("global orchestrator: reconcileServiceOneNode batch failed")
	}
}

// restartTask calls the restart supervisor's Restart function, which
// sets a task's desired state to shutdown and restarts it if the restart
// policy calls for it to be restarted.
func (g *GlobalOrchestrator) restartTask(ctx context.Context, taskID string, serviceID string) {
	err := g.store.Update(func(tx store.Tx) error {
		t := store.GetTask(tx, taskID)
		if t == nil || t.DesiredState > api.TaskStateRunning {
			return nil
		}
		service := store.GetService(tx, serviceID)
		if service == nil {
			return nil
		}
		return g.restarts.Restart(ctx, tx, g.cluster, service, *t)
	})
	if err != nil {
		log.G(ctx).WithError(err).Errorf("global orchestrator: restartTask transaction failed")
	}
}

func (g *GlobalOrchestrator) removeTask(ctx context.Context, batch *store.Batch, t *api.Task) {
	// set existing task DesiredState to TaskStateShutdown
	// TODO(aaronl): optimistic update?
	err := batch.Update(func(tx store.Tx) error {
		t = store.GetTask(tx, t.ID)
		if t != nil && t.DesiredState < api.TaskStateShutdown {
			t.DesiredState = api.TaskStateShutdown
			return store.UpdateTask(tx, t)
		}
		return nil
	})
	if err != nil {
		log.G(ctx).WithError(err).Errorf("global orchestrator: removeTask failed to remove %s", t.ID)
	}
}

func (g *GlobalOrchestrator) addTask(ctx context.Context, batch *store.Batch, service *api.Service, nodeID string) {
	task := newTask(g.cluster, service, 0, nodeID)

	err := batch.Update(func(tx store.Tx) error {
		return store.CreateTask(tx, task)
	})
	if err != nil {
		log.G(ctx).WithError(err).Errorf("global orchestrator: failed to create task")
	}
}

func (g *GlobalOrchestrator) removeTasks(ctx context.Context, batch *store.Batch, tasks []*api.Task) {
	for _, t := range tasks {
		g.removeTask(ctx, batch, t)
	}
}

func isTaskRunning(t *api.Task) bool {
	return t != nil && t.DesiredState <= api.TaskStateRunning && t.Status.State <= api.TaskStateRunning
}

func isTaskCompleted(t *api.Task, restartPolicy api.RestartPolicy_RestartCondition) bool {
	if t == nil || isTaskRunning(t) {
		return false
	}
	return restartPolicy == api.RestartOnNone ||
		(restartPolicy == api.RestartOnFailure && t.Status.State == api.TaskStateCompleted)
}

func isTaskTerminated(t *api.Task) bool {
	return t != nil && t.Status.State > api.TaskStateRunning
}

func isGlobalService(service *api.Service) bool {
	if service == nil {
		return false
	}
	_, ok := service.Spec.GetMode().(*api.ServiceSpec_Global)
	return ok
}
