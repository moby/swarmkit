package manager

import (
	"time"
	"sort"

	"github.com/docker/swarmkit/api"
	"github.com/docker/swarmkit/log"
	"github.com/docker/swarmkit/manager/orchestrator"
	"github.com/docker/swarmkit/manager/scheduler"
	"github.com/docker/swarmkit/manager/state/raft"
	"github.com/docker/swarmkit/manager/state/store"
	"golang.org/x/net/context"
)

const roleReconcileInterval = 5 * time.Second

// roleManager reconciles the raft member list with desired role changes.

type roleManager struct {
	ctx    context.Context
	cancel func()

	store    *store.MemoryStore
	raft     *raft.Node
	doneChan chan struct{}

	// pending contains changed nodes that have not yet been reconciled in
	// the raft member list.
	pending map[string]*api.Node
	// autoswarmServices has all the global services in the cluster, indexed by ServiceID
	roleManagerServices map[string]*api.Service
	serviceHistory	[]string
	nodes map[string]*api.Node
	managers map[string]*api.Node
	workers map[string]*api.Node
	specifiedManagers	int

}

// newRoleManager creates a new roleManager.
func newRoleManager(store *store.MemoryStore, raftNode *raft.Node) *roleManager {
	ctx, cancel := context.WithCancel(context.Background())
	return &roleManager{
		ctx:      ctx,
		cancel:   cancel,
		store:    store,
		raft:     raftNode,
		doneChan: make(chan struct{}),
		pending:  make(map[string]*api.Node),
		roleManagerServices: make(map[string]*api.Service),
		serviceHistory:	make([]string)
		nodes:    make(map[string]*api.Node),
		managers: make(map[string]*api.Node),
		workers:  make(map[string]*api.Node),
		specifiedManagers:	1,
	}
}

// Run is roleManager's main loop.
// ctx is only used for logging.
func (rm *roleManager) Run(ctx context.Context) {
	defer close(rm.doneChan)

	var (
		nodes    []*api.Node
		ticker   *time.Ticker
		tickerCh <-chan time.Time
	)

	// Watch changes to services and tasks
	queue := rm.store.WatchQueue()
	serviceWatcher, cancel := queue.Watch()
	defer cancel()

	nodeWatcher, cancelWatch, err := store.ViewAndWatch(rm.store,
		func(readTx store.ReadTx) error {
			var err error
			nodes, err = store.FindNodes(readTx, store.All)
			managers, err = store.FindNodes(readTx, store.ByRole(api.NodeRoleManager))
			workers, err = store.FindNodes(readTx, store.ByRole(api.NodeRoleWorker))
			return err
		},
		api.EventUpdateNode{})
	defer cancelWatch()

	if err != nil {
		log.G(ctx).WithError(err).Error("failed to check nodes for role changes")
	} else {
		for _, node := range nodes {
			rm.pending[node.ID] = node
			rm.reconcileRole(ctx, node)
		}
		if len(rm.pending) != 0 {
			ticker = time.NewTicker(roleReconcileInterval)
			tickerCh = ticker.C
		}
	}

	// Lookup autoswarm services
	var existingServices []*api.Service
	rm.store.View(func(readTx store.ReadTx) {
		existingServices, err = store.FindServices(readTx, store.All)
	})
	if err != nil {
		return err
	}

	var reconcileServices []*api.Service
	for _, s := range existingServices {
		if orchestrator.IsRoleManagerService(s) {
			rm.updateService(s)
		}
	}


	for {
		select {
		case event := <-serviceWatcher:
				// TODO(stevvooe): Use ctx to limit running time of operation.
				switch v := event.(type) {
				case api.EventCreateService:
					rm.updateService(ctx, v.Service)
				case api.EventUpdateService:
					rm.updateService(ctx, v.Service)
				case api.EventDeleteService:
					rm.deleteService()
				}

		case rm.specifiedManagers > uint64(len(rm.managers)):
			// TODO check for quorum loss, timeout wait for manager restart/rejoin to maintain quorum integrity

			log.G(ctx).Debugf("Manager Nodes were scaled up from %d to %d instances", rm.managers, rm.specifiedManagers)
			managerScheduler = scheduler.New(store)
			managerScheduler.ScheduleManager(ctx, service.Task, rm.managers, rm.workers)

		case rm.specifiedManagers < uint64(len(rm.managers)):
			// Remove Leader from demote list and demote random Manager to Worker Role.
			// TODO sort demote list by Preferences, health, raft consistency, etc. to favor demoting weaker nodes.
			log.G(ctx).Debugf("Manager Nodes were scaled down from %d to %d instances", rm.managers, rm.specifiedManagers)
			for _, m := range managers {
				if !m.ManagerStatus.Leader {
					err := store.Update(func(tx store.Tx) error {
								demotedNode := store.GetNode(tx, m.ID)
								demotedNode.Spec.DesiredRole = api.NodeRoleWorker
								return store.UpdateNode(tx, demotedNode)
					}
					break
				}
			}

		case event := <-nodeWatcher:
			node := event.(api.EventUpdateNode).Node
			rm.pending[node.ID] = node
			rm.reconcileRole(ctx, node)
			if len(rm.pending) != 0 && ticker == nil {
				ticker = time.NewTicker(roleReconcileInterval)
				tickerCh = ticker.C
			}
		case <-tickerCh:
			for _, node := range rm.pending {
				rm.reconcileRole(ctx, node)
			}
			if len(rm.pending) == 0 {
				ticker.Stop()
				ticker = nil
				tickerCh = nil
			}
		case <-rm.ctx.Done():
			if ticker != nil {
				ticker.Stop()
			}
			return
		}
	}
}

func (rm *roleManager) reconcileRole(ctx context.Context, node *api.Node) {
	if node.Role == node.Spec.DesiredRole {
		// Nothing to do.
		delete(rm.pending, node.ID)
		return
	}

	// Promotion can proceed right away.
	if node.Spec.DesiredRole == api.NodeRoleManager && node.Role == api.NodeRoleWorker {
		err := rm.store.Update(func(tx store.Tx) error {
			updatedNode := store.GetNode(tx, node.ID)
			if updatedNode == nil || updatedNode.Spec.DesiredRole != node.Spec.DesiredRole || updatedNode.Role != node.Role {
				return nil
			}
			updatedNode.Role = api.NodeRoleManager
			return store.UpdateNode(tx, updatedNode)
		})
		if err != nil {
			log.G(ctx).WithError(err).Errorf("failed to promote node %s", node.ID)
		} else {
			delete(rm.pending, node.ID)
		}
	} else if node.Spec.DesiredRole == api.NodeRoleWorker && node.Role == api.NodeRoleManager {
		// Check for node in memberlist
		member := rm.raft.GetMemberByNodeID(node.ID)
		if member != nil {
			// Quorum safeguard
			if !rm.raft.CanRemoveMember(member.RaftID) {
				// TODO(aaronl): Retry later
				log.G(ctx).Debugf("can't demote node %s at this time: removing member from raft would result in a loss of quorum", node.ID)
				return
			}

			rmCtx, rmCancel := context.WithTimeout(rm.ctx, 5*time.Second)
			defer rmCancel()

			if member.RaftID == rm.raft.Config.ID {
				// Don't use rmCtx, because we expect to lose
				// leadership, which will cancel this context.
				log.G(ctx).Info("demoted; transferring leadership")
				err := rm.raft.TransferLeadership(context.Background())
				if err == nil {
					return
				}
				log.G(ctx).WithError(err).Info("failed to transfer leadership")
			}
			if err := rm.raft.RemoveMember(rmCtx, member.RaftID); err != nil {
				// TODO(aaronl): Retry later
				log.G(ctx).WithError(err).Debugf("can't demote node %s at this time", node.ID)
			}
			return
		}

		err := rm.store.Update(func(tx store.Tx) error {
			updatedNode := store.GetNode(tx, node.ID)
			if updatedNode == nil || updatedNode.Spec.DesiredRole != node.Spec.DesiredRole || updatedNode.Role != node.Role {
				return nil
			}
			updatedNode.Role = api.NodeRoleWorker

			return store.UpdateNode(tx, updatedNode)
		})
		if err != nil {
			log.G(ctx).WithError(err).Errorf("failed to demote node %s", node.ID)
		} else {
			delete(rm.pending, node.ID)
		}
	}
}

func (rm *roleManager) reconcileServices(ctx context.Context) {
		latest := serviceHistory[len(serviceHistory)-1]
		service := rm.roleManagerServices[latest]
		deploy := service.Spec.GetMode().(*api.ServiceSpec_Manager)
		rm.specifiedManagers := deploy.Replicated.Replicas
}

func (rm *roleManager) updateService(ctx context.Context, service *api.Service) {
	if !orchestrator.IsRoleManagerService(v.Service) {
		continue
	}
	rm.roleManagerServices[service.ID] = service
	for h := range serviceHistory {
		if h == service.ID {
			serviceHistory = append(serviceHistory[:h], serviceHistory[h+1:]...)
		}
	}
	serviceHistory = append(serviceHistory, service.ID)
	rm.reconcileServices(ctx)
}

func (rm *roleManager) deleteService(ctx context.Context, service *api.Service) {
	if !orchestrator.IsRoleManagerService(v.Service) {
		continue
	}
	rm.roleManagerServices[service.ID] = nil
	for h := range serviceHistory {
		if h == service.ID {
			serviceHistory = append(serviceHistory[:h], serviceHistory[h+1:]...)
		}
	}
	rm.reconcileServices(ctx)
}

// Stop stops the roleManager and waits for the main loop to exit.
func (rm *roleManager) Stop() {
	rm.cancel()
	<-rm.doneChan
}
