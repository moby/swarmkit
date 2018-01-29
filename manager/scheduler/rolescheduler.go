package scheduler

import (
	"time"

	"github.com/docker/swarmkit/api"
	"github.com/docker/swarmkit/api/genericresource"
	"github.com/docker/swarmkit/log"
	"github.com/docker/swarmkit/manager/state"
	"github.com/docker/swarmkit/manager/state/raft/"
	"github.com/docker/swarmkit/manager/state/raft/transport"
	"github.com/docker/swarmkit/manager/state/store"
	"github.com/docker/swarmkit/protobuf/ptypes"
	"golang.org/x/net/context"
)

const (
	// monitorFailures is the lookback period for counting failures of
	// a task to determine if a node is faulty for a particular service.
	monitorFailures = 5 * time.Minute

	// maxFailures is the number of failures within monitorFailures that
	// triggers downweighting of a node in the sorting function.
	maxFailures = 5
)

// roleScheduler holds nodeSet, services, and healthWathcers to schedule node role changes.
type roleScheduler struct {
	ctx    						context.Context
	cancel						func()
	store           	*store.MemoryStore
	services					map[string]*api.Service
	serviceHistory		[]string
	// Pass *raftNode of Leader in manager.go through the parent *Scheduler to use .Transport for healthCheck.
	healthChecker				healthChecker
	managers					map[]*api.Node
	pendingManagers		map[]*api.Node
	// nodeSet from parent Scheduler, use task-driven resources data for role scheduling
	nodeSet        	  *nodeSet
	transport					*transport.Transport
}

// New creates a new scheduler.
func newRoleScheduler(ctx context.Context, store *store.MemoryStore, nodeSet *nodeSet, raftNode *raft.Node) *roleScheduler {
	ctx, cancel := context.WithCancel(ctx)
	healthChecker := newhealthChecker(ctx, raftNode)
	return &roleScheduler{
		store:						store,
		services:					make(map[string]*api.Service),
		serviceHistory:		make([]string),
		managers:					make(map[string]*api.Node),
		pendingManagers:	make(map[string]*api.Node),
		healthChecker:		healthChecker,
		nodeSet:					nodeSet,
		transport:				raftNode.transport,
	}
}

// Run is the roleScheduler event loop. It runs on a parent *Scheduler and uses its referenced
// nodeSet for scheduling new manager roles on nodes with greater resource availability.
// roleScheduler incorporates orchestrator, scheduler, and dispatcher functions into a single
// loop because *api.Task loses the RoleScheduler flag that *api.Service.Spec.Mode provides, so
// passing around Tasks between functions would not do.
// Role scheduling is handled by RunRoleScheduler changing the DesiredRole of a Node,
// and reconciliation is handled by role_manager.go as any other API or CLI role change request.
func (rs *RoleScheduler) Run(ctx context.Context) error {
	defer close(s.doneChan)

	// Watch for updates
	updates, cancel, err := store.ViewAndWatch(rs.store, rs.init)
	if err != nil {
		log.G(ctx).WithError(err).Errorf("snapshot store update failed")
		return err
	}
	defer cancel()

	// Watch for changes.
	for {
		if rs.currentService() != nil {
			rs.scheduleRoles()
		}
		select {
		case event := <-updates:
			switch v := event.(type) {
			case api.EventCreateService:
				rs.createOrUpdateService(v.Service)
			case api.EventUpdateService
				rs.createOrUpdateService(v.Service)
			case api.EventDeleteService:
				rs.deleteService(v.Service)
			case api.EventCreateNode:
				rs.createOrUpdateNode(v.Node)
			case api.EventUpdateNode:
				rs.createOrUpdateNode(v.Node)
			case api.EventDeleteNode:
				rs.removeManager(v.Node)
			}
		case <-rs.ctx.Done():
			return nil
		}
	}
}

func (rs *roleScheduler) init(tx store.ReadTx) error {
	services, err = store.FindServices(tx, store.All)
	if err != nil {
		return err
	}
	for _, s := range services {
			rs.updateService(s)
		}
	}

	nodes, err := store.FindNodes(tx, store.All)
	if err != nil {
		return err
	}
	for _, n := range nodes {
		if n.Role == api.NodeRoleManager {
			rs.managers[node.ID] = node
		}
	}
	return nil
}

func (rs *roleScheduler) createOrUpdateService(service *api.Service) {
	if orchestrator.IsRoleSchedulerService(service) {
		continue
	}
	rs.services[service.ID] = service
	for _, h := range serviceHistory {
		if h == service.ID {
			serviceHistory = append(serviceHistory[:h], serviceHistory[h+1:]...)
		}
	}
	serviceHistory = append(serviceHistory, service.ID)
}

func (rs *roleScheduler) deleteService(service *api.Service) {
	if orchestrator.IsRoleSchedulerService(service) {
		continue
	}
	delete(rs.services, service.ID)
	for _, h := range serviceHistory {
		if h == service.ID {
			serviceHistory = append(serviceHistory[:h], serviceHistory[h+1:]...)
		}
	}
}

func (rs *roleScheduler) currentService(service *api.Service) (service *api.Service){
	if len(rs.services) != 0 {
		return rs.services[len(rs.services)-1]
	}
	return nil
}

func (rs *roleScheduler) createOrUpdateNode(n *api.Node) {
	switch n.Role {
	case api.NodeRoleManager:
		rs.managers[n.ID] = n
	case api.NodeRoleWorker:
		rs.removeManager(n)
		switch n.Spec.DesiredRole {
		case api.NodeRoleManager:
			rs.markPending(n)
		case api.NodeRoleWorker:
			rs.removePending(n)
		}
	}
}

func (rs *roleScheduler) markManager(n *api.Node) {
	rs.managers[n.ID] = n
	rs.removePending(n)
	rs.nodeSet[n.ID].ActiveTasksCountByService[rs.currentService.ID] = 1
}

func (rs *roleScheduler) markPending(n *api.Node) {
	rs.pendingManagers[n.ID] = n
	rs.removeManager(n)
}

func (rs *roleScheduler) removeManager(n *api.Node) {
	if rs.managers[n.ID] != nil {
		rs.managers.remove(n.ID)
	}
	for _, s := range rs.services {
		if rs.nodeSet[n.ID].ActiveTasksCountByService[s.ID] != nil {
			delete(rs.nodeSet[n.ID].ActiveTasksCountByService, s.ID)
		}
	}
}

func (rs *roleScheduler) removePending(n *api.Node) {
	if rs.pendingManagers[n.ID] != nil {
		delete(rs.pendingManagers, n.ID)
	}
}

func (rs *roleScheduler) clearPending() {
	for _, p := range rs.pendingManagers {
		rs.removePending(p)
	}
}

func (rs *roleScheduler) specifiedManagers() uint32 {
	return rs.currentService().Spec.GetMode().(*api.ServiceSpec_Manager).Replicated.Replicas
}

func (rs *roleScheduler) activeManagers() uint32 {
	var active := 0
	for _, m := rs.managers {
		if rs.healthCheck.Active(m.ID) {
			active++
			continue
		}
		rs.nodeSet[m.ID].taskFailed(rs.ctx, rs.currentService().Spec.Task)
	}
	return active
}

func (rs *roleScheduler) scheduleRoles() {
	switch {
	case rs.activeManagers() < rs.specifiedManagers():
		rs.promoteWorkers(rs.specifiedManagers()-rs.activeManagers())
	case rs.activeManagers() > rs.specifiedManagers():
		rs.demoteManagers(rs.activeManagers()-rs.specifiedManagers())
	}
}

func (rs *roleScheduler) promoteWorkers(rolesRequested uint32) {
	var prefs []*api.PlacementPreference
	if t.Spec.Placement != nil {
		prefs = t.Spec.Placement.Preferences
	}

	tree := rs.createDecisionTree(prefs)
	setRole := api.NodeRoleManager
	try := rs.scheduleNRolesOnTree(rolesRequested, setRole, &tree)
// TODO (foxxxyben) change Task to ignore resource reservations so that first try pass
// prefers nodes with fewer running Tasks to be drained, but retry pass doesn't care
// retry := rs.scheduleNRolesOnTree(rolesRequested, setRole, &tree)
// TODO report, explain failed attempts
}

func (rs *roleScheduler) demoteManagers(rolesRequested uint32) {
	var prefs []*api.PlacementPreference
	if t.Spec.Placement != nil {
		prefs = t.Spec.Placement.Preferences
	}
	for i := 0; i < len(prefs)/2; i++ {
			j := len(prefs) - i - 1
			prefs[i], prefs[j] = prefs[j], prefs[i]
		}

	tree := rs.createDecisionTree(prefs)
	setRole := api.NodeRoleManager
	retry := rs.scheduleNRolesOnTree(rolesRequested, setRole, &tree)
	// TODO report, explain failed attempts
}

func (rs *roleScheduler) createDecisionTree(prefs []*api.PlacementPreference) tree *decisionTree {
	rs.pipeline.SetTask(rs.currentService().Spec.Task)

	now := time.Now()

	nodeLess := func(a *NodeInfo, b *NodeInfo) bool {
		// If either node has at least maxFailures recent failures,
		// that's the deciding factor.
		recentFailuresA := a.countRecentFailures(now, t)
		recentFailuresB := b.countRecentFailures(now, t)

		if recentFailuresA >= maxFailures || recentFailuresB >= maxFailures {
			if recentFailuresA > recentFailuresB {
				return false
			}
			if recentFailuresB > recentFailuresA {
				return true
			}
		}
		if recentFailuresA != recentFailuresB {
			return recentFailuresA < recentFailuresB
		}
		// Total number of tasks breaks ties.
		return a.ActiveTasksCount < b.ActiveTasksCount
	}

	return s.nodeSet.tree(t.ServiceID, prefs, rolesRequested, s.pipeline.Process, nodeLess)
}

func (rs *roleScheduler) scheduleNRolesOnTree(rolesRequested int, setRole *api.DesiredRole, tree *decisionTree) int {
	rolesScheduled := 0
	func rolesRemaining() {return rolesRequested - rolesScheduled}
	level := 0
	var levelMap []map[string]*decisionTree
	levelMap[level]["root"] = tree

	// climb tree one level at a time
	for rolesRemaining() > 0 && len(levelMap) => level; level++ {
		var leaves [][]NodeInfo
		var i 0
		// populate leaves on branches
		for _, branch := range levelMap[level]; i++ {
			leaves[i] := branch.orderedNodes(s.pipeline.Process, nodeLess)
		}
		// round-robin iterator
		func round(robin int) bool {
			 if robin == i % len(leaves) {
				 return true
			 }
		 }
		robinCh = make(chan int len(leaves))
		for robin, branch := range leaves {
			go func() {
				for _, leaf := range leaves; rolesRemaining() > 0 && round(robin) {
					if leaf.Node.Spec.DesiredRole != setRole {
						leaf.Node.Spec.DesiredRole = setRole
						rolesScheduled++
						i++
					}
				}
				robinCh <- 0
			} ()
		}
		for ch := 0; rolesRemaining() > 0 || ch < len(leaves); ch++ { <- robinCh }
		// populate branches in next level
		if rolesRemaining() > 0 {
			for _, branch := range levelMap[level] {
				for _, next := range branch.next {
					append(levelMap[level + 1], next)
				}
			}
		}
	}
	return rolesScheduled
}

// Stop causes the scheduler event loop to stop running.
func (rs *roleScheduler) Stop() {
	close(rs.stopChan)
	<-rs.doneChan
}
