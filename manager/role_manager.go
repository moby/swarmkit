package manager

import (
	"time"

	"github.com/docker/swarmkit/api"
	"github.com/docker/swarmkit/log"
	"github.com/docker/swarmkit/manager/orchestrator"
	"github.com/docker/swarmkit/manager/state/raft"
	"github.com/docker/swarmkit/manager/state/raft/membership"
	"github.com/docker/swarmkit/manager/scheduler"
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
	services map[string]*api.Service
	serviceHistory	[]string
	specifiedManagers	uint64
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
		services:	make(map[string]*api.Service),
		serviceHistory:		make([]string),
	}
}

// Run is roleManager's main loop.
// ctx is only used for logging.
func (rm *roleManager) Run(ctx context.Context) {
	defer close(rm.doneChan)

	// Init tickerCh
	ticker = time.NewTicker(roleReconcileInterval)
	tickerCh = ticker.C

	// Init serviceWatcher
	var existingServices []*api.Service
	serviceWatcher, cancelServiceWatcher, err := store.ViewAndWatch(rm.store,
		func(readTx store.ReadTx) error {
			var err error
			existingServices, err = store.FindServices(readTx, store.All)
			}
			return err
		}
	)
	defer cancelServiceWatcher()

	if err != nil {
		log.G(ctx).WithError(err).Error("failed to check services for role changes")
	} else {
		for _, service := range existingServices {
			if orchestrator.IsRoleManagerService(service) {
				rm.updateService(service)
			}
	}

	// Init nodeWatcher
	var existingNodes []*api.Node
	nodeWatcher, cancelNodeWatcher, err := store.ViewAndWatch(rm.store,
		func(readTx store.ReadTx) error {
			var err error
			nodes, err = store.FindNodes(readTx, store.All)
			return err
		}
	)
	defer cancelNodeWatcher()

	if err != nil {
		log.G(ctx).WithError(err).Error("failed to check nodes for role changes")
	} else {
		for _, node := range nodes {
			rm.pending[node.ID] = node
			rm.reconcileRole(ctx, node)
		}
	}

// main loop
	for {
		select {
		case event := <-serviceWatcher:
			reconcileServices(event)
		case event := <-nodeWatcher:
			node := event.(api.EventUpdateNode).Node
			rm.pending[node.ID] = node
			rm.reconcileRole(ctx, node)
		case specifiedManagers > uint64(len(rm.raft.cluster.members)):
			// TODO check for quorum loss, timeout wait for manager restart/rejoin to maintain quorum integrity
			log.G(ctx).Debugf("Manager Nodes were scaled up from %d to %d instances", rm.raft.cluster.members, rm.specifiedManagers)
			managerScheduler = scheduler.New(store)
			managerScheduler.ScheduleManager(ctx, service.Task, rm.raft)
		case specifiedManagers < uint64(len(rm.raft.cluster.members)):
			// Remove Leader from demote list and demote random Manager to Worker Role.
			// TODO sort demote list by Preferences, health, raft consistency, etc. to favor demoting weaker nodes.
			log.G(ctx).Debugf("Manager Nodes were scaled down from %d to %d instances", rm.raft.cluster.members, rm.specifiedManagers)
			for _, m := range rm.raft.cluster.members {
				if !m.Status.Leader {
					err := store.Update(func(tx store.Tx) error {
								demotedNode := store.GetNode(tx, m.NodeID)
								demotedNode.Spec.DesiredRole = api.NodeRoleWorker
								return store.UpdateNode(tx, demotedNode)
					}
					break
				}
			}
		case <-tickerCh:
			rm.reconcileManagers(ctx)
			for _, node := range rm.pending {
				rm.reconcileRole(ctx, node)
			}
		case <-rm.ctx.Done():
			ticker.Stop()
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

func (rm *roleManager) reconcileServices() {
	latest := serviceHistory[len(serviceHistory)-1]
	service := rm.services[latest.ID]
	deploy := service.Spec.GetMode().(*api.ServiceSpec_Manager)
	specifiedManagers := deploy.Replicated.Replicas
}

func (rm *roleManager) updateService(service *api.Service) {
	if orchestrator.IsRoleManagerService(service) {
		continue
	}
	rm.roleManagerServices[service.ID] = service
	for h := range serviceHistory {
		if h == service.ID {
			serviceHistory = append(serviceHistory[:h], serviceHistory[h+1:]...)
		}
	}
	serviceHistory = append(serviceHistory, service.ID)
	rm.reconcileServices()
}

func (rm *roleManager) deleteService(service *api.Service) {
	if orchestrator.IsRoleManagerService(service) {
		continue
	}
	rm.roleManagerServices[service.ID] = nil
	for h := range serviceHistory {
		if h == service.ID {
			serviceHistory = append(serviceHistory[:h], serviceHistory[h+1:]...)
		}
	}
	reconcileServices()
}

// Stop stops the roleManager and waits for the main loop to exit.
func (rm *roleManager) Stop() {
	rm.cancel()
	<-rm.doneChan
}
