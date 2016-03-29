package allocator

import (
	"fmt"

	"github.com/Sirupsen/logrus"
	"github.com/docker/go-events"
	"github.com/docker/swarm-v2/api"
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

func (a *Allocator) doNetworkInit() error {
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
	err = a.store.View(func(tx state.ReadTx) error {
		var err error
		networks, err = tx.Networks().Find(state.All)
		if err != nil {
			return fmt.Errorf("error listing all networks in store while trying to allocate during init: %v", err)
		}

		return nil
	})
	if err != nil {
		return err
	}

	for _, n := range networks {
		if na.IsAllocated(n) {
			continue
		}

		if err := a.allocateNetwork(nc, n); err != nil {
			logrus.Errorf("failed allocating network %s during init: %v", n.ID, err)
		}
	}

	// Allocate tasks in the store so far before we started watching.
	if err := a.store.Update(func(tx state.Tx) error {
		tasks, err := tx.Tasks().Find(state.All)
		if err != nil {
			return fmt.Errorf("error listing all tasks in store while trying to allocate during init: %v", err)
		}

		for _, t := range tasks {
			// No container or network configured. Not interested.
			if t.Spec.GetContainer() == nil {
				continue
			}

			if len(t.Spec.GetContainer().Networks) == 0 {
				continue
			}

			if err := a.allocateTask(nc, tx, t); err != nil {
				logrus.Errorf("failed allocating task %s during init: %v", t.ID, err)
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

		if err := a.allocateNetwork(nc, n); err != nil {
			logrus.Errorf("Failed allocation for network %s: %v", n.ID, err)
			break
		}

		// We successfully allocated a network. Time to revisit unallocated tasks.
		a.procUnallocatedTasks(nc)
	case state.EventDeleteNetwork:
		n := v.Network

		// The assumption here is that all dependent objects
		// have been cleaned up when we are here so the only
		// thing that needs to happen is free the network
		// resources.
		if err := nc.nwkAllocator.Deallocate(n); err != nil {
			logrus.Errorf("Failed during network free for network %s: %v", n.ID, err)
		}
	case state.EventCreateTask, state.EventUpdateTask, state.EventDeleteTask, state.EventCommit:
		a.doTaskAlloc(nc, ev)
	}
}

func (a *Allocator) doTaskAlloc(nc *networkContext, ev events.Event) {
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
		a.procUnallocatedTasks(nc)
		return
	}

	// No container or network configured. Not interested.
	if t.Spec.GetContainer() == nil {
		return
	}

	if len(t.Spec.GetContainer().Networks) == 0 {
		return
	}

	if !nc.nwkAllocator.IsTaskAllocated(t) {
		if isDelete {
			delete(nc.unallocatedTasks, t.ID)
			return
		}

		nc.unallocatedTasks[t.ID] = t
		return
	}

	// If the task has stopped running or it's being deleted then
	// we should free the network resources associated with the
	// task.
	if t.Status.State > api.TaskStateRunning || isDelete {
		if err := nc.nwkAllocator.DeallocateTask(t); err != nil {
			logrus.Errorf("Failed freeing network resources for task %s: %v", t.ID, err)
		}
	}
}

func (a *Allocator) allocateNetwork(nc *networkContext, n *api.Network) error {
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
			logrus.Errorf("Failed rolling back allocation for network %s: %v", n.ID, err)
		}

		return err
	}

	return nil
}

func (a *Allocator) allocateTask(nc *networkContext, tx state.Tx, t *api.Task) error {
	// We might be here even if a task allocation has already
	// happened but wasn't successfully committed to store. In such
	// cases skip allocation and go straight ahead to updating the
	// store.
	if !nc.nwkAllocator.IsTaskAllocated(t) {
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
		storeT.Status.State = api.TaskStateAllocated
	}

	storeT.Networks = t.Networks
	if err := tx.Tasks().Update(storeT); err != nil {
		return fmt.Errorf("failed updating state in store transaction for task %s: %v", storeT.ID, err)
	}

	return nil
}

func (a *Allocator) procUnallocatedTasks(nc *networkContext) {
	if err := a.store.Update(func(tx state.Tx) error {
		for _, t := range nc.unallocatedTasks {
			if err := a.allocateTask(nc, tx, t); err != nil {
				return fmt.Errorf("task allocation failure: %v", err)
			}

			// If we are here then the allocation of the task and
			// the subsequent update of the store was
			// successfull. No need to remember this task any
			// more.
			delete(nc.unallocatedTasks, t.ID)
		}
		return nil
	}); err != nil {
		logrus.Error(err)
	}
}
