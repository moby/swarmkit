package allocator

import (
	"context"
	"sync"
	"time"

	"github.com/docker/docker/pkg/plugingetter"
	"github.com/docker/go-events"
	metrics "github.com/docker/go-metrics"

	"github.com/docker/swarmkit/manager/allocator/network"
	"github.com/docker/swarmkit/manager/allocator/network/errors"

	"github.com/docker/swarmkit/api"
	"github.com/docker/swarmkit/log"
	"github.com/docker/swarmkit/manager/state/store"
	"github.com/docker/swarmkit/protobuf/ptypes"
)

const (
	// AllocatedStatusMessage is the message added to a task status after the
	// task is successfully allocated
	AllocatedStatusMessage = "pending task scheduling"

	maxBatchInterval = 500 * time.Millisecond
)

var (
	batchProcessingDuration metrics.Timer

	allocatorActions metrics.LabeledTimer
)

func init() {
	ns := metrics.NewNamespace("swarmkit", "allocator", nil)

	allocatorActions = ns.NewLabeledTimer(
		"allocator_actions",
		"The number of seconds it takes the allocator to perform some action",
		"object", "action",
	)

	batchProcessingDuration = ns.NewTimer(
		"batch_duration",
		"The number of seconds it takes to process a full batch of allocations",
	)

	metrics.Register(ns)
}

// Allocator is the top-level component controlling resource allocation for
// swarm objects
type Allocator struct {
	store   *store.MemoryStore
	network network.Allocator

	// fields that make running the allocator thread-safe.
	runOnce sync.Once

	// fields to make stopping the allocator possible and safe through the stop
	// method
	stop     chan struct{}
	stopped  chan struct{}
	stopOnce sync.Once

	// pending objects are those which have not yet passed through the
	// allocator.
	pendingMu       sync.Mutex
	pendingNetworks map[string]struct{}
	pendingTasks    map[string]struct{}
	pendingNodes    map[string]struct{}
	// no need for a pending services map. we will allocate services lazily
	// before we allocate a task

	// deletedObjects is a list of all object IDs that have been found to be
	// deleted in the last iteration of processPendingAllocations. It should
	// be protected for concurrent access with pendingMu.
	//
	// When allocating new resources in the processPendingAllocations function,
	// we release the store lock while we process the allocations. This means
	// an object could be deleted in between the time we read it from the store
	// and the time we're ready to write it back. We need to deallocate any
	// resources we've allocated for the object. Importantly, this means we may
	// have resources allocated for the object that are not committed to the
	// store, and which we cannot commit to the store because the object is
	// gone. Therefore, we deallocate the object when we see that it has
	// failed.
	//
	// However, the object deletion will also show up in the event stream, at
	// which time, without this map, we would try to deallocate the object
	// again. This results in a double free, and could mark resources freed
	// when they're actually in use. Importantly, we can't just defer
	// deallocating until we receive this event, because the event will include
	// an old version of the object, before it had passed through allocation.
	//
	// To prevent this case, if any objects are deleted while we're processing
	// allocations, we will add those objects ids to this map. Then, when
	// running through the event stream, we will check if an object is in this
	// map before deallocating it; if so, we will skip deallocation, and we
	// will remove the entry from the map so as not to have the map growing
	// infinitely.
	//
	// Object IDs are unique even across different object types, so we can keep
	// them all in the same map to avoid the trouble of holding a different map
	// for every object.
	deletedObjects map[string]struct{}
}

// New creates an Allocator object
func New(store *store.MemoryStore, pg plugingetter.PluginGetter) *Allocator {
	a := &Allocator{
		store:           store,
		stop:            make(chan struct{}),
		stopped:         make(chan struct{}),
		network:         network.NewAllocator(pg),
		pendingNetworks: map[string]struct{}{},
		pendingTasks:    map[string]struct{}{},
		pendingNodes:    map[string]struct{}{},
		deletedObjects:  map[string]struct{}{},
	}
	return a
}

// Run starts this allocator, if it has not yet been started. The Allocator can
// only be run once.
func (a *Allocator) Run(ctx context.Context) error {
	// initialize the variable err to be already started. this will get set to
	// some other value (or even nil) when the allocator's run function exits,
	// but any subsequent calls will not pass through the Once, and will return
	// this error
	err := errors.ErrBadState("allocator already started")
	a.runOnce.Do(func() {
		err = a.run(ctx)
		// close the stopped channel to indicate that the allocator has fully
		// exited.
		close(a.stopped)
	})
	return err
}

func (a *Allocator) run(ctx context.Context) error {
	// General overview of how this function works:
	//
	// Run is a shim between the asynchronous store interface, and the
	// synchronous allocator interface. It uses a map to keep track of which
	// objects have outstanding allocations to perform, and uses a goroutine to
	// synchronize reads and writes with this map and allow it to function as a
	// a source of work.
	//
	// The first thing it does is read the object store and pass all of the
	// objects currently available to the network allocator. The network
	// allocator's restore function will add all allocated objects to the local
	// state so we can proceed with new allocations.
	//
	// It thens adds all objects in the store to the working set, so that any
	// objects currently in the store that aren't allocated can be.
	//
	// Then, it starts up two major goroutines:
	//
	// The first is the goroutine that gets object ids out of the work pile and
	// performs allocation on them. If the allocation succeeds, it writes that
	// allocation to raft. If it doesn't succeed, the object is added back to
	// the work pile to be serviced later
	//
	// The second is the goroutine that services events off the event queue. It
	// reads incoming store events and grabs just the ID and object type, and
	// adds that to the work pile. We only deal with the ID, not the full
	// object because the full object may have changed since the event came in
	// The exception in this event loop is for deallocations. When an object is
	// deleted, the event we receive is our last chance to deal with that
	// object. In that case, we immediately call into Deallocate.

	ctx, c := context.WithCancel(ctx)
	// defer canceling the context, so that anything waiting on it will exit
	// when this routine exits.
	defer c()
	ctx = log.WithModule(ctx, "allocator")
	ctx = log.WithField(ctx, "method", "(*Allocator).Run")
	log.G(ctx).Info("starting network allocator")

	// set up a goroutine for stopping the allocator from the Stop method. this
	// just cancels the context if that channel is closed.
	go func() {
		select {
		case <-ctx.Done():
		case <-a.stop:
			c()
		}
	}()

	// we want to spend as little time as possible in transactions, because
	// transactions stop the whole cluster, so we're going to grab the lists
	// and then get out
	var (
		networks []*api.Network
		services []*api.Service
		tasks    []*api.Task
		nodes    []*api.Node
	)
	watch, cancel, err := store.ViewAndWatch(a.store,
		func(tx store.ReadTx) error {
			// NOTE(dperny): FindByAll
			// In the call stack for Find<Whatever> by store.All, the only
			// possibility for errors lies deep within memdb, and seems to be a
			// consequence of some kind of misconfiguration of some sort of the
			// indexes or tables. Those errors should not be possible, so we
			// ignore them to simplify the code. If they do occur, then your
			// swarm is hosed anyway
			networks, _ = store.FindNetworks(tx, store.All)
			services, _ = store.FindServices(tx, store.All)
			tasks, _ = store.FindTasks(tx, store.All)
			nodes, _ = store.FindNodes(tx, store.All)

			return nil
		},
		api.EventCreateNetwork{},
		api.EventDeleteNetwork{},
		api.EventDeleteService{},
		api.EventCreateTask{},
		api.EventUpdateTask{},
		api.EventDeleteTask{},
		api.EventCreateNode{},
		api.EventDeleteNode{},
	)
	if err != nil {
		// if this returns an error, we cannot proceed with the allocator. we
		// need to crash it to avoid getting into bad states
		return err
	}

	// set up a routine to cancel the event stream when the context is canceled
	go func() {
		select {
		case <-ctx.Done():
			log.G(ctx).Debug("context done, canceling the event stream")
			cancel()
		}
	}()

	// now restore the local state
	log.G(ctx).Info("restoring local network allocator state")
	if err := a.network.Restore(networks, services, tasks, nodes); err != nil {
		log.G(ctx).WithError(err).Error("error restoring local network allocator state")
		// if restore fails, we cannot proceed with the allocator. we need to
		// stop it here, so that we don't perform work and get into a bad
		// state.
		return err
	}

	// allocate all of the store in its current state. we do this becauses at
	// this level, we have no idea which objects are allocated or not; only the
	// network package and below knows. before we can start allocating new
	// stuff off the event stream, however, we need to allocate any old stuff
	// that wasn't allocated before we started. so, we add every object to the
	// pending map. those that are already allocated will simply return
	// ErrAlreadyAllocated when we try to allocate them, which we can ignore,
	// and those that aren't will be. after this first pass, we'll be in a
	// consistent state ready to start processing new allocations.
	for _, network := range networks {
		a.pendingNetworks[network.ID] = struct{}{}
	}
	for _, node := range nodes {
		a.pendingNodes[node.ID] = struct{}{}
	}
	for _, task := range tasks {
		a.pendingTasks[task.ID] = struct{}{}
	}

	log.G(ctx).Info("processing all outstanding allocations in the store")
	a.processPendingAllocations(ctx)
	log.G(ctx).Info("finished processing all pending allocations")

	var wg sync.WaitGroup
	wg.Add(2)

	log.G(ctx).Debug("starting event watch loop")
	// this goroutine handles incoming store events. all we need from the
	// events is the ID of what has been updated. by the time we service the
	// allocation, the object may have changed, so we don't save any other
	// information. we'll get to it later.
	go func() {
		// shadow context. trivial optimization to avoid calling this in every
		// single event handle body
		ctx := log.WithField(ctx, "method", "(*Allocator).handleEvent")
		defer wg.Done()
		// TODO(dperny): docker/swarmkit#2619: there is an outstanding issue
		// with a deadlock that can result from closing the store at the same
		// time the callback watch is canceled. This can cause the tests to
		// deadlock. It may also be a concern during node demotion.
		for {
			select {
			case <-ctx.Done():
				return
			case event := <-watch:
				a.handleEvent(ctx, event)
			}
		}
	}()

	log.G(ctx).Debug("starting batch processing loop")
	// allocations every 500 milliseconds
	batchTimer := time.NewTimer(maxBatchInterval)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-ctx.Done():
				return
			case <-batchTimer.C:
				a.pendingMu.Lock()
				total := len(a.pendingNetworks) + len(a.pendingNodes) + len(a.pendingTasks)
				a.pendingMu.Unlock()
				// we release the lock before checking the total, which is fine
				// because the worst case is something gets deleted and the
				// processPendingAllocations just jumps straight through
				if total > 0 {
					// every batch interval, do all allocations and then reset the
					// timer to ready for the next batch
					a.processPendingAllocations(ctx)
				}
				batchTimer.Reset(maxBatchInterval)
			}
		}
	}()

	log.G(ctx).Info("allocator is started")
	wg.Wait()
	log.G(ctx).Info("allocator is finished")
	defer log.G(ctx).Debug("all defers exited")
	return nil
}

// Stop is a method that stops the running allocator, blocking until it has
// fully stopped
func (a *Allocator) Stop() {
	a.stopOnce.Do(func() {
		close(a.stop)
	})
	// NOTE(dperny) an un-selected channel read is inescapable and can result
	// in deadlocks, however, being able to escape this channel read with, say,
	// a context cancelation, would just lead to resource leaks of an already
	// deadlocked allocator, so the risk of a deadlock isn't that important
	<-a.stopped
}

// handleEvent contains the logic for handling a single event off of the event
// stream
func (a *Allocator) handleEvent(ctx context.Context, event events.Event) {
	// grab the lock for the whole body. there is no benefit to more granular
	// locking, because very little of what is happening here can proceed
	// without holding this lock.
	a.pendingMu.Lock()
	defer a.pendingMu.Unlock()
	switch ev := event.(type) {
	case api.EventCreateNetwork:
		// get the network
		n := ev.Network
		if n != nil {
			a.pendingNetworks[n.ID] = struct{}{}
		}
	case api.EventDeleteNetwork:
		// if the user deletes  the network, we don't have to do any store
		// actions, and we can't return any errors. The network is already
		// gone, deal with it
		if ev.Network == nil {
			return
		}
		// check if this network was already deallocated
		if _, ok := a.deletedObjects[ev.Network.ID]; ok {
			// if it is, remove it from the deleted objects map, because we'll
			// only get 1 delete event for it and we need to stop this map from
			// growing forever.
			delete(a.deletedObjects, ev.Network.ID)
			// then, return. there is nothing to do. the network was already
			// deallocated
			return
		}

		allocDone := metrics.StartTimer(allocatorActions.WithValues("network", "deallocate"))
		err := a.network.DeallocateNetwork(ev.Network)
		allocDone()
		if err != nil {
			log.G(ctx).WithError(err).WithField("network.id", ev.Network.ID).Error("error deallocating network")
		}
		delete(a.pendingNetworks, ev.Network.ID)
	// case api.EventCreateService, api.EventUpdateService:
	//     the case for create and update services is not needed.  we will
	//     lazy-allocate services. the reason for this is to guarantee that the
	//     latest version of the service is always allocated, and the task is
	//     always using the latest version of the service for allocation.
	case api.EventDeleteService:
		if ev.Service == nil {
			// if there is no service, nothing to do
			return
		}
		if _, ok := a.deletedObjects[ev.Service.ID]; ok {
			delete(a.deletedObjects, ev.Service.ID)
			return
		}
		allocDone := metrics.StartTimer(allocatorActions.WithValues("service", "deallocate"))
		err := a.network.DeallocateService(ev.Service)
		allocDone()
		if err != nil {
			log.G(ctx).WithField("service.id", ev.Service.ID).WithError(err).Error("error deallocating service")
		}
	case api.EventCreateTask, api.EventUpdateTask:
		var t *api.Task
		if e, ok := ev.(api.EventUpdateTask); ok {
			// before we do any processing, check the old task. if the previous
			// state was a terminal state, then the task must necessarily have
			// already been deallocated.
			if e.OldTask != nil && !isTaskLive(e.OldTask) {
				return
			}
			t = e.Task
		} else {
			t = ev.(api.EventCreateTask).Task
		}

		if t == nil {
			// if for some reason there is no task, nothing to do
			return
		}

		// updating a task may mean the task has entered a terminal state. if
		// it has, we will free its network resources just as if we had deleted
		// it.
		if !isTaskLive(t) {
			// if the task was already in a terminal state, we should ignore
			// this update, not try to deallocate it again
			if _, ok := a.deletedObjects[t.ID]; ok {
				delete(a.deletedObjects, t.ID)
				return
			}
			log.G(ctx).WithField("task.id", t.ID).Debug("deallocating task")
			allocDone := metrics.StartTimer(allocatorActions.WithValues("task", "deallocate"))
			err := a.network.DeallocateTask(t)
			allocDone()
			if err != nil {
				log.G(ctx).WithField("task.id", t.ID).WithError(err).Error("error deallocating task")
			}
			// if the task was pending before now, it certainly isn't anymore
			delete(a.pendingTasks, t.ID)
			// we'll need to reallocate this node next time around, so that we
			// drop this network attachment
			if t.NodeID != "" {
				a.pendingNodes[t.NodeID] = struct{}{}
			}
		} else {
			a.pendingTasks[t.ID] = struct{}{}
		}
	case api.EventDeleteTask:
		// if there is no task in the event, nothing to do
		if ev.Task == nil {
			return
		}
		if !isTaskLive(ev.Task) {
			// if the task is being deleted from a terminal state, we will have
			// deallocated it when it moved into that terminal state, and do
			// not need to do so now.
			return
		}
		if _, ok := a.deletedObjects[ev.Task.ID]; ok {
			// if the task has already been deleted and deallocated, there is
			// no need to do so now. remove it from the list tracking
			// deallocations.
			delete(a.deletedObjects, ev.Task.ID)
			return
		}
		allocDone := metrics.StartTimer(allocatorActions.WithValues("task", "deallocate"))
		err := a.network.DeallocateTask(ev.Task)
		allocDone()
		if err != nil {
			log.G(ctx).WithField("task.id", ev.Task.ID).WithError(err).Error("error deallocating task")
		}
		// add the node that this task was assigned to to the pendingNodes, so
		// we can deallocate the network attachment on that node if necessary
		if ev.Task.NodeID != "" {
			a.pendingNodes[ev.Task.NodeID] = struct{}{}
		}
		delete(a.pendingTasks, ev.Task.ID)
	case api.EventCreateNode:
		if ev.Node != nil {
			a.pendingNodes[ev.Node.ID] = struct{}{}
		}
	// case api.EventUpdateNode:
	//     we don't need the update node case because nothing about
	//     a node update will require reallocation.
	case api.EventDeleteNode:
		if ev.Node == nil {
			// if the node is nil, nothing to do
			return
		}
		if _, ok := a.deletedObjects[ev.Node.ID]; ok {
			delete(a.deletedObjects, ev.Node.ID)
			return
		}
		a.network.DeallocateNode(ev.Node)
		delete(a.pendingNodes, ev.Node.ID)
	}
}

func (a *Allocator) processPendingAllocations(ctx context.Context) {
	ctx = log.WithField(ctx, "method", "(*Allocator).processPendingAllocations")

	// we need to hold the lock the whole time we're allocating.
	a.pendingMu.Lock()
	defer a.pendingMu.Unlock()

	log.G(ctx).Debugf(
		"processing pending allocations. %v networks, %v nodes, %v tasks",
		len(a.pendingNetworks), len(a.pendingNodes), len(a.pendingTasks),
	)

	defer func() {
		log.G(ctx).Debugf(
			"finished processing pending allocations. retrying: %v networks, %v nodes, %v tasks",
			len(a.pendingNetworks), len(a.pendingNodes), len(a.pendingTasks),
		)
	}()

	// collect metrics on how long each batch of pending allocations takes.
	// start at timer after we acquire the lock, and then defer the done call.
	done := metrics.StartTimer(batchProcessingDuration)
	defer done()

	// capture the pending maps and reinitialize them, so they'll be clean
	// after this runs
	pendingNetworks := a.pendingNetworks
	a.pendingNetworks = map[string]struct{}{}
	pendingNodes := a.pendingNodes
	a.pendingNodes = map[string]struct{}{}
	pendingTasks := a.pendingTasks
	a.pendingTasks = map[string]struct{}{}
	// first, pending networks. open the store and grab all of the pending
	// networks. networks can't be updated, so we don't need to worry about a
	// race condition.

	// if no networks are pending, skip this block
	if len(pendingNetworks) > 0 {
		// we'll need two slices. the first is to hold all of the networks we get
		// from the store. the second holds all of the successfully allocated
		// networks
		networks := make([]*api.Network, 0, len(pendingNetworks))
		allocatedNetworks := make([]*api.Network, 0, len(pendingNetworks))

		a.store.View(func(tx store.ReadTx) {
			for id := range pendingNetworks {
				nw := store.GetNetwork(tx, id)
				if nw == nil {
					log.G(ctx).WithField("network.id", id).Debug("network not found, probably deleted")
					continue
				}
				networks = append(networks, nw)
			}
		})

		// now, allocate each network
		for _, network := range networks {
			ctx := log.WithField(ctx, "network.id", network.ID)
			allocDone := metrics.StartTimer(allocatorActions.WithValues("network", "allocate"))
			err := a.network.AllocateNetwork(network)
			allocDone()

			// error conditions from the allocator are numerous, but are
			// constrained to a few specific types, which we can completely cover.
			switch {
			case err == nil:
				// first, if no error occurred, then we can add this to the list of
				// networks we will commit
				allocatedNetworks = append(allocatedNetworks, network)
			case errors.IsErrAlreadyAllocated(err):
				// if the network is already allocated, there is nothing to do. log
				// that and do not add this network to the batch
				log.G(ctx).Debug("network already fully allocated")
			case errors.IsErrRetryable(err):
				// if the error is retryable, then we should log that error and
				// re-add this network to our pending networks
				log.G(ctx).WithError(err).Error("network could not be allocated, but will be retried")
				a.pendingNetworks[network.ID] = struct{}{}
			default:
				// default covers any other error case. specifically, error that
				// are not retryable. for these, we should fail and not re-add them
				// to the pending map. they will never succeed
				log.G(ctx).WithError(err).Error("network cannot be allocated")
			}
		}

		// again, if we haven't actually successfully allocated any networks,
		// we should skip this part to avoid holding the lock
		if len(allocatedNetworks) > 0 {
			// now, batch update the networks we just allocated
			if err := a.store.Batch(func(batch *store.Batch) error {
				for _, network := range allocatedNetworks {
					if err := batch.Update(func(tx store.Tx) error {
						ctx := log.WithField(ctx, "network.id", network.ID)
						// first, get the network and make sure it still exists
						currentNetwork := store.GetNetwork(tx, network.ID)
						if currentNetwork == nil {
							log.G(ctx).WithField("network.id", network.ID).Debugf("network was deleted after allocation but before commit")
							// if the current network is nil, then the network was
							// deleted and we should deallocate it.
							a.network.DeallocateNetwork(network)
							a.deletedObjects[network.ID] = struct{}{}
						}
						if err := store.UpdateNetwork(tx, network); err != nil {
							return err
						}
						return nil
					}); err != nil {
						log.G(ctx).WithError(err).Error("error writing network to store")
					}
				}
				return nil
			}); err != nil {
				log.G(ctx).WithError(err).Error("error committing allocated networks")
			}
		}
	}

	// finally, allocate all tasks.
	if len(pendingTasks) > 0 {
		// this might seem a bit nonobvious. basically, what we're doing is
		// allocating services as needed by tasks. and we're going to optimize
		// on allocating many tasks belonging to a service at the same time.
		// So, in our View transaction, we're going to go through each pending
		// task ID. if it's still ready for allocation, we're going to get its
		// service and stash the service in a slice. then, we're going to stash
		// the task in a slice in a map.

		// services is a slice of services with tasks pending allocation
		services := []*api.Service{}

		// tasks is a map from service id to a slice of task objects belonging
		// to that service
		tasks := map[string][]*api.Task{}
		a.store.View(func(tx store.ReadTx) {
			for taskid := range pendingTasks {
				t := store.GetTask(tx, taskid)
				// if the task is nil, then that means it has been deleted
				if t != nil {
					// only new tasks need to be allocated. However, if a task
					// has a node assignment, we should update that now.
					if api.TaskStateNew < t.DesiredState && t.DesiredState <= api.TaskStateRunning {
						if t.Status.State == api.TaskStateNew {
							// if the task has an empty service, then we don't need to
							// allocate the service. however, we'll keep those
							// serviceless tasks (network attachment tasks) under the
							// emptystring key in the map and specialcase that key
							// later.
							service := store.GetService(tx, t.ServiceID)
							if service != nil {
								services = append(services, service)
							}
							// thus, if t.ServiceID == "", this will still work
							// this is safe without initializing each slice, because
							// appending to a nil slice is valid
							tasks[t.ServiceID] = append(tasks[t.ServiceID], t)
						} else if t.NodeID != "" {
							a.network.AddTaskNode(t)
							// we'll need to reallocate the node
							pendingNodes[t.NodeID] = struct{}{}
						}
					} else {
						log.G(ctx).WithField("task.id", taskid).Debug("task is no longer in a state requiring allocation")
					}
				} else {
					log.G(ctx).WithField("task.id", taskid).Debug("pending task is not in store, probably deleted")
				}
			}
		})

		// allocatedServices will store the services we have actually
		// allocated, not including the services that didn't need allocation
		allocatedServices := make([]*api.Service, 0, len(services))
		// allocatedTasks, likewise, stores the tasks we've successfully
		// allocated and should commit. we don't initialize it with a fixed
		// length because it's trickier for tasks
		allocatedTasks := []*api.Task{}

		// now, allocate the services and all of their tasks
		for _, service := range services {
			ctx := log.WithField(ctx, "service.id", service.ID)
			allocDone := metrics.StartTimer(allocatorActions.WithValues("service", "allocate"))
			err := a.network.AllocateService(service)
			allocDone()
			switch {
			case err == nil:
				// first, if no error occurred, then we can add this to the list of
				// networks we will commit
				allocatedServices = append(allocatedServices, service)
			case errors.IsErrAlreadyAllocated(err):
				// if the network is already allocated, there is nothing to do. log
				// that and do not add this network to the batch
				log.G(ctx).Debug("service already fully allocated")
			case errors.IsErrRetryable(err):
				// if the error is retryable, then we should log that error,
				// and then re-add every pending task belonging to this service
				// to our pending tasks, and remove their entry from the tasks
				// map.
				// TODO(dperny): we may also want to update the task status
				// with the error message, but that behavior is out of scope
				// for the initial allocator rewrite.
				log.G(ctx).WithError(err).Error("service could not be allocated, but will be retried")
				for _, task := range tasks[service.ID] {
					a.pendingTasks[task.ID] = struct{}{}
				}
				delete(tasks, service.ID)
			default:
				// default covers any other error case. specifically, error that
				// are not retryable. for these, we should fail and not re-add them
				// to the pending map. they will never succeed. additionally,
				// remove these tasks from the map
				log.G(ctx).WithError(err).Error("service cannot be allocated")
				delete(tasks, service.ID)
			}

			// now, we should allocate all of the tasks for this service
			for _, task := range tasks[service.ID] {
				ctx := log.WithField(ctx, "task.id", task.ID)
				allocTaskDone := metrics.StartTimer(allocatorActions.WithValues("task", "allocate"))
				err := a.network.AllocateTask(task)
				allocTaskDone()
				switch {
				case err == nil:
					// first, if no error occurred, then we can add this to the
					// list of tasks we will commit
					allocatedTasks = append(allocatedTasks, task)
				case errors.IsErrAlreadyAllocated(err):
					// if the task is already allocated, there is nothing to do. log
					// that and do not add this network to the batch
					log.G(ctx).Debug("task already fully allocated")
				case errors.IsErrRetryable(err):
					// if the error is retryable, then we should log that error
					// and readd the task to the pending tasks map
					log.G(ctx).WithError(err).Error("task could not be allocated, but will be retried")
					a.pendingTasks[task.ID] = struct{}{}
				default:
					// default covers any other error case. specifically, error that
					// are not retryable. for these, we should fail and not re-add them
					// to the pending map. they will never succeed. additionally,
					// remove these tasks from the map
					log.G(ctx).WithError(err).Error("task cannot be allocated")
				}

				// if the task has a node assignment, we need to add that node
				// to pendingNodes, because we may need to update that node's
				// allocations
				if task.NodeID != "" {
					pendingNodes[task.NodeID] = struct{}{}
				}
			}
		}

		// now handle all of the tasks belonging to no service
		for _, task := range tasks[""] {
			ctx := log.WithField(ctx, "task.id", task.ID)
			allocTaskDone := metrics.StartTimer(allocatorActions.WithValues("task", "allocate"))
			err := a.network.AllocateTask(task)
			allocTaskDone()
			switch {
			case err == nil:
				// first, if no error occurred, then we can add this to the
				// list of tasks we will commit
				allocatedTasks = append(allocatedTasks, task)
			case errors.IsErrAlreadyAllocated(err):
				// if the task is already allocated, there is nothing to do. log
				// that and do not add this network to the batch
				log.G(ctx).Debug("task already fully allocated")
			case errors.IsErrRetryable(err):
				// if the error is retryable, then we should log that error
				// and readd the task to the pending tasks map
				log.G(ctx).WithError(err).Error("task could not be allocated, but will be retried")
				a.pendingTasks[task.ID] = struct{}{}
			default:
				// default covers any other error case. specifically, error that
				// are not retryable. for these, we should fail and not re-add them
				// to the pending map. they will never succeed. additionally,
				// remove these tasks from the map
				log.G(ctx).WithError(err).Error("task cannot be allocated")
			}
			// if the task has a node assignment, we need to add that node
			// to pendingNodes, because we may need to update that node's
			// allocations
			if task.NodeID != "" {
				pendingNodes[task.NodeID] = struct{}{}
			}
		}

		// finally, if we have any services or tasks to commit, open a
		// batch and commit them
		if len(allocatedServices)+len(allocatedTasks) > 0 {
			if err := a.store.Batch(func(batch *store.Batch) error {
				for _, service := range allocatedServices {
					if err := batch.Update(func(tx store.Tx) error {
						currentService := store.GetService(tx, service.ID)
						if currentService == nil {
							// NOTE(dperny): see explanation in node
							log.G(ctx).WithField("service.id", service.ID).Debug("service was delete after allocation but before commit")
							a.network.DeallocateService(service)
							a.deletedObjects[service.ID] = struct{}{}
							return nil
						}
						// the service may have changed in the meantime, so
						// only update the fields we just altered in it.
						// in this case, only the endpoint
						currentService.Endpoint = service.Endpoint
						// then commit the service
						return store.UpdateService(tx, currentService)
					}); err != nil {
						// TODO(dperny): i'm unsure how to handle batch
						// failures, other than maybe crashing the allocator.
						// Batch covers up what nodes actually succeeded and
						// what failed.
					}
				}

				for _, task := range allocatedTasks {
					if err := batch.Update(func(tx store.Tx) error {
						currentTask := store.GetTask(tx, task.ID)
						if !isTaskLive(currentTask) {
							log.G(ctx).WithField("task.id", task.ID).Debug("task terminated or removed after allocation but before commit")
							a.network.DeallocateTask(task)
							a.deletedObjects[task.ID] = struct{}{}
							return nil
						}

						currentTask.Endpoint = task.Endpoint
						currentTask.Networks = task.Networks
						// update the task status as well
						currentTask.Status = api.TaskStatus{
							State:     api.TaskStatePending,
							Message:   AllocatedStatusMessage,
							Timestamp: ptypes.MustTimestampProto(time.Now()),
						}
						return store.UpdateTask(tx, currentTask)
					}); err != nil {
						// TODO(dperny): see batch update above
					}
				}
				return nil
			}); err != nil {
				// TODO(dperny): see batch update above.
			}
		}
	}

	if len(pendingNodes) > 0 {
		nodes := make([]*api.Node, 0, len(pendingNodes))
		allocatedNodes := make([]*api.Node, 0, len(pendingNodes))

		// get the freshest copy of the node.
		a.store.View(func(tx store.ReadTx) {
			for nodeID := range pendingNodes {
				node := store.GetNode(tx, nodeID)
				if node == nil {
					log.G(ctx).WithField("node.id", nodeID).Debug("node not found, probably deleted")
					continue
				}
				nodes = append(nodes, node)
			}
		})

		// now go and try to allocate each node
		for _, node := range nodes {
			ctx := log.WithField(ctx, "node.id", node.ID)
			allocDone := metrics.StartTimer(allocatorActions.WithValues("node", "allocate"))
			err := a.network.AllocateNode(node)
			allocDone()

			switch {
			case err == nil:
				allocatedNodes = append(allocatedNodes, node)
			case errors.IsErrAlreadyAllocated(err):
				// if the network is already allocated, there is nothing to do. log
				// that and do not add this network to the batch
				log.G(ctx).Debug("node already fully allocated")
			case errors.IsErrRetryable(err):
				// if the error is retryable, then we should log that error and
				// re-add this network to our pending networks
				log.G(ctx).WithError(err).Error("node could not be allocated, but will be retried")
				a.pendingNodes[node.ID] = struct{}{}
			default:
				// default covers any other error case. specifically, error that
				// are not retryable. for these, we should fail and not re-add them
				// to the pending map. they will never succeed
				log.G(ctx).WithError(err).Error("node cannot be allocated")
			}
		}

		// if any nodes successfully allocated, commit them
		if len(allocatedNodes) > 0 {
			if err := a.store.Batch(func(batch *store.Batch) error {
				for _, node := range allocatedNodes {
					if err := batch.Update(func(tx store.Tx) error {
						currentNode := store.GetNode(tx, node.ID)
						if currentNode == nil {
							// if there is no node, then it must have been
							// deleted. deallocate our changes.

							log.G(ctx).WithField("node.id", node.ID).Debug("node was deleted after allocation but before committing")
							a.network.DeallocateNode(node)
							a.deletedObjects[node.ID] = struct{}{}
							return nil
						}

						// the node may have changed in the meantime since we
						// read it before allocation. but since we're the only
						// one who changes the attachments, we can just plop
						// our new attachments into that slice with no danger
						currentNode.Attachments = node.Attachments

						return store.UpdateNode(tx, currentNode)
					}); err != nil {
						log.G(ctx).WithError(err).WithField("node.id", node.ID).Error("error in committing allocated node")
					}
				}
				return nil
			}); err != nil {
				log.G(ctx).WithError(err).Error("error in batch update of allocated nodes")
			}
		}
	}
}

// isTaskLive returns true if the task is non-nil and in a non-terminal state.
//
// this function, though simple, has been factored out to avoid any errors that
// may arise from slightly different checks for liveness (for example,
// task.Status.State > api.TaskStateRunning vs task.Status.State >= api.TaskStateCompleted
func isTaskLive(t *api.Task) bool {
	// a nil task is not live, of course
	if t == nil {
		return false
	}
	// a task past the RUNNING state is not live.
	if t.Status.State > api.TaskStateRunning {
		return false
	}
	// otherwise, it's live
	return true
}
