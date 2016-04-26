package allocator

import (
	"fmt"

	"github.com/docker/go-events"
	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/log"
	"github.com/docker/swarm-v2/manager/allocator/networkallocator"
	"github.com/docker/swarm-v2/manager/state"
	"golang.org/x/net/context"
)

const (
	// Network allocator Voter ID for task allocation vote.
	networkVoter = "network"
)

// Network context information which is used throughout the network allocation code.
type networkContext struct {
	// Instance of the low-level network allocator which performs
	// the actual network allocation.
	nwkAllocator *networkallocator.NetworkAllocator

	// A table of unallocated tasks which will be revisited if any thing
	// changes in system state that might help task allocation.
	unallocatedTasks map[string]*api.Task
}

func (a *Allocator) doNetworkInit(ctx context.Context) error {
	na, err := networkallocator.New()
	if err != nil {
		return err
	}

	nc := &networkContext{
		nwkAllocator:     na,
		unallocatedTasks: make(map[string]*api.Task),
	}

	// Allocate networks in the store so far before we started watching.
	var networks []*api.Network
	a.store.View(func(tx state.ReadTx) {
		networks, err = tx.Networks().Find(state.All)
	})
	if err != nil {
		return fmt.Errorf("error listing all networks in store while trying to allocate during init: %v", err)
	}

	for _, n := range networks {
		if na.IsAllocated(n) {
			continue
		}

		if err := a.allocateNetwork(ctx, nc, n); err != nil {
			log.G(ctx).Errorf("failed allocating network %s during init: %v", n.ID, err)
		}
	}

	// Allocate services in the store so far before we process watched events.
	var services []*api.Service
	a.store.View(func(tx state.ReadTx) {
		services, err = tx.Services().Find(state.All)
	})
	if err != nil {
		return fmt.Errorf("error listing all services in store while trying to allocate during init: %v", err)
	}

	for _, s := range services {
		if s.Spec.Endpoint == nil {
			continue
		}

		if na.IsServiceAllocated(s) {
			continue
		}

		if err := a.allocateService(ctx, nc, s); err != nil {
			log.G(ctx).Errorf("failed allocating service %s during init: %v", s.ID, err)
		}
	}

	// Allocate tasks in the store so far before we started watching.
	var tasks []*api.Task
	a.store.View(func(tx state.ReadTx) {
		tasks, err = tx.Tasks().Find(state.All)
	})
	if err != nil {
		return fmt.Errorf("error listing all tasks in store while trying to allocate during init: %v", err)
	}

	if _, err := a.store.Batch(func(batch state.Batch) error {
		for _, t := range tasks {
			if taskDead(t) {
				continue
			}

			// No container or network configured. Not interested.
			if t.Spec.GetContainer() == nil {
				continue
			}

			if len(t.Spec.GetContainer().Networks) == 0 {
				continue
			}

			if nc.nwkAllocator.IsTaskAllocated(t) {
				continue
			}

			err := batch.Update(func(tx state.Tx) error {
				return a.allocateTask(ctx, nc, tx, t)
			})
			if err != nil {
				log.G(ctx).Errorf("failed allocating task %s during init: %v", t.ID, err)
				nc.unallocatedTasks[t.ID] = t
			}
		}

		return nil
	}); err != nil {
		return err
	}

	a.netCtx = nc
	return nil
}

func (a *Allocator) doNetworkAlloc(ctx context.Context, ev events.Event) {
	nc := a.netCtx

	switch v := ev.(type) {
	case state.EventCreateNetwork:
		n := v.Network
		if nc.nwkAllocator.IsAllocated(n) {
			break
		}

		if err := a.allocateNetwork(ctx, nc, n); err != nil {
			log.G(ctx).Errorf("Failed allocation for network %s: %v", n.ID, err)
			break
		}

		// We successfully allocated a network. Time to revisit unallocated tasks.
		a.procUnallocatedTasks(ctx, nc)
	case state.EventDeleteNetwork:
		n := v.Network

		// The assumption here is that all dependent objects
		// have been cleaned up when we are here so the only
		// thing that needs to happen is free the network
		// resources.
		if err := nc.nwkAllocator.Deallocate(n); err != nil {
			log.G(ctx).Errorf("Failed during network free for network %s: %v", n.ID, err)
		}
	case state.EventCreateService:
		s := v.Service

		// No endpoint configuration. No network allocation needed.
		if s.Spec.Endpoint == nil {
			break
		}

		if nc.nwkAllocator.IsServiceAllocated(s) {
			break
		}

		if err := a.allocateService(ctx, nc, s); err != nil {
			log.G(ctx).Errorf("Failed allocation for service %s: %v", s.ID, err)
			break
		}

		// We successfully allocated a service. Time to revisit unallocated tasks.
		a.procUnallocatedTasks(ctx, nc)
	case state.EventUpdateService:
		s := v.Service

		// No endpoint configuration and no endpoint state. Nothing to do.
		if s.Spec.Endpoint == nil && s.Endpoint == nil {
			break
		}

		if nc.nwkAllocator.IsServiceAllocated(s) {
			break
		}

		if err := a.allocateService(ctx, nc, s); err != nil {
			log.G(ctx).Errorf("Failed allocation during update of service %s: %v", s.ID, err)
			break
		}

		// We successfully allocated a service. Time to revisit unallocated tasks.
		a.procUnallocatedTasks(ctx, nc)
	case state.EventDeleteService:
		s := v.Service

		// No endpoint configuration. No network allocation needed.
		if s.Spec.Endpoint == nil {
			break
		}

		if !nc.nwkAllocator.IsServiceAllocated(s) {
			break
		}

		if err := nc.nwkAllocator.ServiceDeallocate(s); err != nil {
			log.G(ctx).Errorf("Failed deallocation during delete of service %s: %v", s.ID, err)
		}
	case state.EventCreateTask, state.EventUpdateTask, state.EventDeleteTask, state.EventCommit:
		a.doTaskAlloc(ctx, nc, ev)
	}
}

// taskRunning checks whether a task is either actively running, or in the
// process of starting up.
func taskRunning(t *api.Task) bool {
	return t.DesiredState <= api.TaskStateRunning && t.Status.State <= api.TaskStateRunning
}

// taskDead checks whether a task is not actively running as far as allocator purposes are concerned.
func taskDead(t *api.Task) bool {
	return t.DesiredState > api.TaskStateRunning && t.Status.State > api.TaskStateRunning
}

func (a *Allocator) doTaskAlloc(ctx context.Context, nc *networkContext, ev events.Event) {
	var (
		isDelete bool
		t        *api.Task
	)

	switch v := ev.(type) {
	case state.EventCreateTask:
		t = v.Task
	case state.EventUpdateTask:
		t = v.Task
	case state.EventDeleteTask:
		isDelete = true
		t = v.Task
	case state.EventCommit:
		a.procUnallocatedTasks(ctx, nc)
		return
	}

	// If the task has stopped running or it's being deleted then
	// we should free the network resources associated with the
	// task right away.
	if taskDead(t) || isDelete {
		if nc.nwkAllocator.IsTaskAllocated(t) {
			if err := nc.nwkAllocator.DeallocateTask(t); err != nil {
				log.G(ctx).Errorf("Failed freeing network resources for task %s: %v", t.ID, err)
			}
		}

		// Cleanup any task references that might exist in unallocatedTasks
		delete(nc.unallocatedTasks, t.ID)
		return
	}

	// No container or network configured. Not interested.
	if t.Spec.GetContainer() == nil {
		return
	}

	var s *api.Service
	if t.ServiceID != "" {
		a.store.View(func(tx state.ReadTx) {
			s = tx.Services().Get(t.ServiceID)
		})
		if s == nil {
			// If the task is running it is not normal to
			// not be able to find the associated
			// service. If the task is not running (task
			// is either dead or the desired state is set
			// to dead) then the service may not be
			// available in store. But we still need to
			// cleanup network resources associated with
			// the task.
			if taskRunning(t) && !isDelete {
				log.G(ctx).Errorf("Event %T: Failed to get service %s for task %s state %s: could not find service %s", ev, t.ServiceID, t.ID, t.Status.State, t.ServiceID)
				return
			}
		}
	}

	// Task has no network attached and it is created for a
	// service which has no endpoint configuration or the service
	// is already allocated. Try to immediately move it to
	// ALLOCATED state.
	if len(t.Spec.GetContainer().Networks) == 0 &&
		(s == nil || s.Spec.Endpoint == nil || nc.nwkAllocator.IsServiceAllocated(s)) {
		// If we are already in allocated state, there is
		// absolutely nothing else to do.
		if t.Status.State >= api.TaskStateAllocated {
			return
		}

		// If the task is not attached to any network, network
		// allocators job is done. Immediately cast a vote so
		// that the task can be moved to ALLOCATED state as
		// soon as possible.
		if err := a.store.Update(func(tx state.Tx) error {
			storeT := tx.Tasks().Get(t.ID)
			if storeT == nil {
				return fmt.Errorf("task %s not found while trying to update state", t.ID)
			}

			// Make sure to save the endpoint in task
			// since we know by now that the service is
			// allocated.
			if s != nil {
				storeT.Endpoint = s.Endpoint.Copy()
			}

			if a.taskAllocateVote(networkVoter, t.ID) {
				storeT.Status.State = api.TaskStateAllocated
			}

			if err := tx.Tasks().Update(storeT); err != nil {
				return fmt.Errorf("failed updating state in store transaction for task %s: %v", storeT.ID, err)
			}

			return nil
		}); err != nil {
			log.G(ctx).WithError(err).Error("error updating task network")
		}

		return
	}

	if !nc.nwkAllocator.IsTaskAllocated(t) ||
		(s != nil && s.Spec.Endpoint != nil && !nc.nwkAllocator.IsServiceAllocated(s)) {

		nc.unallocatedTasks[t.ID] = t
	}
}

func (a *Allocator) allocateService(ctx context.Context, nc *networkContext, s *api.Service) error {
	if err := nc.nwkAllocator.ServiceAllocate(s); err != nil {
		return err
	}

	if err := a.store.Update(func(tx state.Tx) error {
		if err := tx.Services().Update(s); err != nil {
			return fmt.Errorf("failed updating state in store transaction for service %s: %v", s.ID, err)
		}
		return nil
	}); err != nil {
		if err := nc.nwkAllocator.ServiceDeallocate(s); err != nil {
			log.G(ctx).WithError(err).Errorf("failed rolling back allocation of service %s: %v", s.ID, err)
		}

		return err
	}

	return nil
}

func (a *Allocator) allocateNetwork(ctx context.Context, nc *networkContext, n *api.Network) error {
	if err := nc.nwkAllocator.Allocate(n); err != nil {
		return fmt.Errorf("failed during network allocation for network %s: %v", n.ID, err)
	}

	if err := a.store.Update(func(tx state.Tx) error {
		if err := tx.Networks().Update(n); err != nil {
			return fmt.Errorf("failed updating state in store transaction for network %s: %v", n.ID, err)
		}
		return nil
	}); err != nil {
		if err := nc.nwkAllocator.Deallocate(n); err != nil {
			log.G(ctx).WithError(err).Errorf("failed rolling back allocation of network %s", n.ID)
		}

		return err
	}

	return nil
}

func (a *Allocator) allocateTask(ctx context.Context, nc *networkContext, tx state.Tx, t *api.Task) error {
	// We might be here even if a task allocation has already
	// happened but wasn't successfully committed to store. In such
	// cases skip allocation and go straight ahead to updating the
	// store.
	if !nc.nwkAllocator.IsTaskAllocated(t) {
		if t.ServiceID != "" {
			s := tx.Services().Get(t.ServiceID)
			if s == nil {
				return fmt.Errorf("could not find service %s", t.ServiceID)
			}

			if s.Spec.Endpoint != nil && !nc.nwkAllocator.IsServiceAllocated(s) {
				return fmt.Errorf("service %s to which this task %s belongs has pending allocations", s.ID, t.ID)
			}

			t.Endpoint = s.Endpoint.Copy()
		}

		for _, na := range t.Spec.GetContainer().Networks {
			n := tx.Networks().Get(na.GetNetworkID())
			if n == nil {
				t.Networks = t.Networks[:0]
				return fmt.Errorf("failed to retrieve network %s while allocating task %s", na.GetNetworkID(), t.ID)
			}

			if !nc.nwkAllocator.IsAllocated(n) {
				t.Networks = t.Networks[:0]
				return fmt.Errorf("network %s attached to task %s not allocated yet", n.ID, t.ID)
			}

			t.Networks = append(t.Networks, &api.Task_NetworkAttachment{Network: n})
		}

		if err := nc.nwkAllocator.AllocateTask(t); err != nil {
			t.Networks = t.Networks[:0]
			return fmt.Errorf("failed during networktask allocation for task %s: %v", t.ID, err)
		}
	}

	// Get the latest task state from the store before updating.
	storeT := tx.Tasks().Get(t.ID)
	if storeT == nil {
		return fmt.Errorf("could not find task %s while trying to update network allocation", t.ID)
	}

	// Update the network allocations and moving to
	// ALLOCATED state on top of the latest store state.
	if a.taskAllocateVote(networkVoter, t.ID) {
		if storeT.Status.State < api.TaskStateAllocated {
			storeT.Status.State = api.TaskStateAllocated
		}
	}

	storeT.Networks = t.Networks
	if err := tx.Tasks().Update(storeT); err != nil {
		return fmt.Errorf("failed updating state in store transaction for task %s: %v", storeT.ID, err)
	}

	return nil
}

func (a *Allocator) procUnallocatedTasks(ctx context.Context, nc *networkContext) {
	tasks := make([]*api.Task, 0, len(nc.unallocatedTasks))

	committed, err := a.store.Batch(func(batch state.Batch) error {
		for _, t := range nc.unallocatedTasks {
			tasks = append(tasks, t)
			err := batch.Update(func(tx state.Tx) error {
				return a.allocateTask(ctx, nc, tx, t)
			})
			if err != nil {
				return fmt.Errorf("task allocation failure: %v", err)
			}
		}

		return nil
	})
	if err != nil {
		log.G(ctx).WithError(err).Error("failed to allocate task")
	}

	for i := 0; i != committed; i++ {
		delete(nc.unallocatedTasks, tasks[i].ID)
	}
}
