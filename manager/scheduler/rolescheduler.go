package scheduler

import (
	"time"

	"github.com/docker/swarmkit/api"
	"github.com/docker/swarmkit/log"
	"github.com/docker/swarmkit/identity"
	"github.com/docker/swarmkit/manager/orchestrator"
	"github.com/docker/swarmkit/manager/state/store"
	"github.com/docker/swarmkit/protobuf/ptypes"
	"golang.org/x/net/context"
)

// TODO add RoleSchedulerConfig to special RoleScheduler api.Service type
const (
	// how often to check for manager failures
	defaultHealthHeartbeat = 15 * time.Second
	// how long to wait for pending managers to become active
	defaultPendingTimeout = 1 * time.Minute
	// how long to wait for a failed manager to recover to prevent quorum loss
	defaultRecoveryTimeout = 1 * time.Minute
	// how often to add an extra manager so force demotion/replacement of lower ranked nodes
	defaultUpgradeInterval = 5 * time.Minute
)

type RoleSchedulerConfig struct {
	healthHeartbeat		time.Duration
	pendingTimeout		time.Duration
	recoveryTimeout		time.Duration
	upgradeInterval		time.Duration
}

// DefaultRoleSchedulerConfig returns default config for RoleScheduler.
func DefaultRoleSchedulerConfig() *RoleSchedulerConfig {
	return &RoleSchedulerConfig{
		healthHeartbeat: defaultHealthHeartbeat,
		pendingTimeout:  defaultPendingTimeout,
		recoveryTimeout: defaultRecoveryTimeout,
		upgradeInterval: defaultUpgradeInterval,
	}
}

// roleScheduler holds nodeSets, services, and Transport to check health and schedule node role changes.
type roleScheduler struct {
	ctx    						context.Context
	cancel						func()
	store           	*store.MemoryStore
	config						*RoleSchedulerConfig
	taskID						string
	services					[]*api.Service

	managers					managerSet
	// nodeSet from parent Scheduler, use task-driven resources data for role scheduling
	nodeSet        	  *nodeSet
	pipeline					*Pipeline

	healthTicker			*time.Ticker
	upgradeTicker			*time.Ticker
}

	type managerSet struct {
		active					nodeSet
		failed					nodeSet
		pending					nodeSet
	}

// New creates a new scheduler.
func newRoleScheduler(ctx context.Context, store *store.MemoryStore, nodeSet *nodeSet) *roleScheduler {
	ctx, cancel := context.WithCancel(ctx)
	config := DefaultRoleSchedulerConfig()
	return &roleScheduler{
		ctx:							ctx,
		cancel:						cancel,
		store:						store,
		config:						config,
		taskID:						identity.NewID(),
		nodeSet:					nodeSet,
		healthTicker:			time.NewTicker(config.healthHeartbeat),
		upgradeTicker:		time.NewTicker(config.upgradeInterval),
		pipeline:         NewPipeline(),

	}
}

// Run is the roleScheduler event loop. It runs on a parent *Scheduler and uses its referenced
// nodeSet for scheduling new manager roles on nodes with greater resource availability.
// roleScheduler incorporates orchestrator, scheduler, and dispatcher functions into a single
// loop because *api.Task loses the RoleScheduler flag that *api.Service.Spec.Mode provides, so
// passing around Tasks between functions would not do.
// Role scheduling is handled by RunRoleScheduler changing the DesiredRole of a Node,
// and reconciliation is handled by role_manager.go as any other API or CLI role change request.
func (rs *roleScheduler) Run(ctx context.Context) error {

	// Watch for updates
	updates, cancel, err := store.ViewAndWatch(rs.store, rs.init)
	if err != nil {
		log.G(ctx).WithError(err).Errorf("snapshot store update failed")
		return err
	}
	defer cancel()

	go rs.scheduleRoles()

	// Watch for changes.
	for {
		select {
		case <-rs.upgradeTicker.C:
			rs.promoteWorkers(1)
		case event := <-updates:
			switch v := event.(type) {
			case api.EventCreateService:
				rs.createOrUpdateService(v.Service)
			case api.EventUpdateService:
				rs.createOrUpdateService(v.Service)
			case api.EventDeleteService:
				rs.deleteService(v.Service)
			case api.EventCreateNode:
				rs.createOrUpdateNode(v.Node)
			case api.EventUpdateNode:
				rs.createOrUpdateNode(v.Node)
			case api.EventDeleteNode:
				rs.removeManager(v.Node.ID)
			}
		case <-rs.ctx.Done():
			rs.upgradeTicker.Stop()
			return nil
		}
	}
}

func (rs *roleScheduler) init(tx store.ReadTx) error {
	services, err := store.FindServices(tx, store.All)
	if err != nil {
		return err
	}
	for _, s := range services {
			rs.createOrUpdateService(s)
		}

	nodes, err := store.FindNodes(tx, store.All)
	if err != nil {
		return err
	}
	for _, n := range nodes {
		if n.Spec.DesiredRole == api.NodeRoleManager {
			if n.Role == api.NodeRoleManager {
				rs.markActive(n.ID)
			} else {
				rs.markPending(n.ID)
			}
		}
	}
	return nil
}

func (rs *roleScheduler) createOrUpdateService(service *api.Service) {
	if orchestrator.IsRoleSchedulerService(service) {
		rs.deleteService(service)
		rs.services = append(rs.services, service)
	}
}

func (rs *roleScheduler) deleteService(service *api.Service) {
	if orchestrator.IsRoleSchedulerService(service) {
		for h, s := range rs.services {
			if s == service {
				rs.services = append(rs.services[:h], rs.services[h+1:]...)
			}
		}
	}
}

func (rs *roleScheduler) currentTask() *api.Task {
		return &api.Task{
		ID:                 rs.taskID,
		ServiceAnnotations: rs.currentService().Spec.Annotations,
		Spec:               rs.currentService().Spec.Task,
		SpecVersion:        rs.currentService().SpecVersion,
		ServiceID:          rs.currentService().ID,
		Status: api.TaskStatus{
			State:     api.TaskStateNew,
			Timestamp: ptypes.MustTimestampProto(time.Now()),
			Message:   "created",
		},
		Endpoint: &api.Endpoint{
			Spec: rs.currentService().Spec.Endpoint.Copy(),
		},
		DesiredState: api.TaskStateRunning,
	}
}

func (rs *roleScheduler) currentService() (service *api.Service){
	if len(rs.services) != 0 {
		return rs.services[len(rs.services)-1]
	} else {
		return nil
	}
}

func (rs *roleScheduler) createOrUpdateNode(n *api.Node) {
	switch n.Spec.DesiredRole {
	case api.NodeRoleWorker:
		rs.removeManager(n.ID)
	case api.NodeRoleManager:
		switch n.Role {
		case api.NodeRoleManager:
			rs.markActive(n.ID)
		case api.NodeRoleWorker:
			rs.markPending(n.ID)
		}
	}
}

func (rs *roleScheduler) removeManager(n string) {
	rs.unmarkActive(n)
	rs.unmarkFailed(n)
	rs.unmarkPending(n)
}

func (rs *roleScheduler) markActive(n string) {
	nodeInfo, err := rs.nodeSet.nodeInfo(n)
	if err == nil {
		rs.managers.active.addOrUpdateNode(nodeInfo)
	}
	nodeInfo.ActiveTasksCountByService[rs.currentService().ID] = 1
	rs.unmarkFailed(n)
	rs.unmarkPending(n)
}

func (rs *roleScheduler) unmarkActive(n string) {
	nodeInfo, err := rs.nodeSet.nodeInfo(n)
	if err == nil {
		rs.managers.active.remove(n)
		for _, s := range rs.services {
			delete(nodeInfo.ActiveTasksCountByService, s.ID)
		}
		// rs.healthTicker.C <- time.Now()
	}
}

func (rs *roleScheduler) markFailed(n string) {
	nodeInfo, err := rs.nodeSet.nodeInfo(n)
	if err == nil {
		rs.managers.failed.addOrUpdateNode(nodeInfo)
	}
	nodeInfo.taskFailed(rs.ctx, rs.currentTask())
	rs.unmarkActive(n)
	rs.unmarkPending(n)
}

func (rs *roleScheduler) unmarkFailed(n string) {
	rs.managers.failed.remove(n)
}

func (rs *roleScheduler) markPending(n string) {
	nodeInfo, err := rs.nodeSet.nodeInfo(n)
	if err == nil {
		rs.managers.pending.addOrUpdateNode(nodeInfo)
		go func (n string) {
			time.Sleep(rs.config.pendingTimeout)
			rs.unmarkPending(n)
		}(n)
	}
	rs.unmarkActive(n)
	rs.unmarkFailed(n)
}

func (rs *roleScheduler) unmarkPending(n string) {
	rs.managers.pending.remove(n)
}

func (rs *roleScheduler) clearReserves() {
	for _, f := range rs.managers.failed.nodes {
		rs.unmarkFailed(f.ID)
	}
	for _, p := range rs.managers.pending.nodes {
		rs.unmarkPending(p.ID)
	}
}

func (rs *roleScheduler) specifiedManagers() int {
	return int(rs.currentService().Spec.GetMode().(*api.ServiceSpec_Manager).Manager.Replicas)
}

func (rs *roleScheduler) activeManagers() int {
	active := 0
	for ID, m := range rs.managers.active.nodes {
		if m.Status.State == api.NodeStatus_READY {
			active++
		} else {
			rs.markFailed(ID)
		}
	}
	return active
}

func (rs *roleScheduler) scheduledManagers() int {
	return rs.activeManagers() + len(rs.managers.pending.nodes)
}

func (rs *roleScheduler) scheduleRoles() {
	for rs.currentService() != nil {
		select {
		case <-rs.healthTicker.C:
			switch {
			case rs.activeManagers() < rs.specifiedManagers():
				switch {
				case rs.scheduledManagers() <= rs.specifiedManagers():
					rs.promoteWorkers(rs.specifiedManagers()-rs.activeManagers())
				case rs.scheduledManagers() < rs.specifiedManagers():
					time.Sleep(rs.config.pendingTimeout)
					rs.promoteWorkers(rs.specifiedManagers()-rs.activeManagers())
				case rs.activeManagers() < (rs.specifiedManagers()/2):
					time.Sleep(rs.config.recoveryTimeout)
					rs.promoteWorkers(rs.specifiedManagers()-rs.activeManagers())
				}
			case rs.activeManagers() > rs.specifiedManagers():
				rs.demoteManagers(rs.activeManagers()-rs.specifiedManagers())
			case rs.activeManagers() == rs.specifiedManagers():
				rs.clearReserves()
			}
			case <-rs.ctx.Done():
				rs.healthTicker.Stop()
				return
		}
	}
}

type roleRequest struct {
	count			int
	search 		api.NodeRole
	set				api.NodeRole
	prefs			[]*api.PlacementPreference
	searchSet	*nodeSet
	proposed	nodeSet
}

func (rs *roleScheduler) promoteWorkers(rolesRequested int) {
	var prefs []*api.PlacementPreference
	if rs.currentTask().Spec.Placement != nil {
		prefs = rs.currentTask().Spec.Placement.Preferences
	}

	request := &roleRequest{
		count:			rolesRequested,
		search:			api.NodeRoleManager,
		set:				api.NodeRoleManager,
		searchSet:	rs.nodeSet,
		prefs:			prefs,
	}
	response := rs.proposeNRolesOnNodes(request)
	for n, _ := range response.proposed.nodes {
		rs.updateDesiredRole(rs.nodeSet.nodes[n], request.set)
		rs.markPending(n)
	}

// TODO (foxxxyben) change Task to ignore resource reservations so that first try pass
// prefers nodes with fewer running Tasks to be drained, but retry pass doesn't care
// retry := rs.scheduleNRolesOnTree(rolesRequested, searchRole, &tree)
// TODO report, explain failed attempts
}

func (rs *roleScheduler) demoteManagers(rolesRequested int) {
	var prefs []*api.PlacementPreference
	if rs.currentTask().Spec.Placement != nil {
		prefs = rs.currentTask().Spec.Placement.Preferences
	}
	for i := 0; i < len(prefs)/2; i++ {
			j := len(prefs) - i - 1
			prefs[i], prefs[j] = prefs[j], prefs[i]
		}

	request := &roleRequest{
		count:			rolesRequested,
		search:			api.NodeRoleManager,
		set:				api.NodeRoleWorker,
		searchSet:	rs.nodeSet,
		prefs:			prefs,
	}

	response := rs.proposeNRolesOnNodes(request)
	for n, _ := range response.proposed.nodes {
		rs.updateDesiredRole(rs.nodeSet.nodes[n], request.set)
		rs.removeManager(n)
	}
	// TODO report, explain failed attempts
}

func (rs *roleScheduler) updateDesiredRole(node NodeInfo, role api.NodeRole) error {
	err := rs.store.Update(func(tx store.Tx) error {
		updatedNode := store.GetNode(tx, node.ID)
		if updatedNode == nil || updatedNode.Spec.DesiredRole != node.Spec.DesiredRole || updatedNode.Role != node.Role {
			return nil
		}
		updatedNode.Spec.DesiredRole = role
		return store.UpdateNode(tx, updatedNode)
	})
	if err != nil {
		log.G(rs.ctx).WithError(err).Errorf("failed to set desired node role %s", node.ID)
		return err
	}
	return nil
}

func (rs *roleScheduler) proposeNRolesOnNodes(req *roleRequest) (res *roleRequest) {
	t := rs.currentTask()
	rs.pipeline.SetTask(t)
	now := time.Now()
	req.proposed.alloc(req.count)
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

	tree := req.searchSet.tree(rs.currentService().ID, req.prefs, req.count, rs.pipeline.Process, nodeLess)

	rolesRemaining := func() int {
		return req.count - len(req.proposed.nodes)
	}

	treeSlice := make([][]*decisionTree, 1)
	treeSlice[0][0] = &tree

	// climb tree one level at a time
	for level := 0; rolesRemaining() > 0 && len(treeSlice) >= level; level++ {
		leaves := make([][]NodeInfo, len(treeSlice[level]))
		leafIterator := make([]int, len(treeSlice[level]))
		levelLeaves := 0
		i := 0
		// populate leaves on branches
		for _, branch := range treeSlice[level] {
			leaves[i] = branch.orderedNodes(rs.pipeline.Process, nodeLess)
			leafIterator[i] = len(leaves[i])
			levelLeaves = levelLeaves + len(leaves[i])
			i++
		}
		// round-robin iterator
		round := func(robin int) int {
			 return robin % len(leaves)
		}
		for robin := 0; rolesRemaining() > 0 && robin < levelLeaves; robin++ {
			branch := round(robin)
			leaf := leaves[branch][leafIterator[branch]]
			if len(leaves[branch]) > leafIterator[branch] {
				if leaf.Spec.DesiredRole != req.search {
					req.proposed.addOrUpdateNode(leaf)
					i++
				}
				leafIterator[branch]++
			} else {
				i++
			}
		}

		// populate branches in next level
		if rolesRemaining() > 0 {
			branchSlice := make([]*decisionTree, 1)
			for _, branch := range treeSlice[level] {
				for _, next := range branch.next {
					branchSlice = append(branchSlice, next)
				}
			}
			treeSlice = append(treeSlice, branchSlice)
		}
	}
	return req
}

// Stop causes the scheduler event loop to stop running.
func (rs *roleScheduler) Stop() {
	rs.cancel()
}
