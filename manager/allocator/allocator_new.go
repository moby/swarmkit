package allocator

import (
	"context"
	"time"

	"github.com/docker/docker/pkg/plugingetter"

	"github.com/docker/swarmkit/manager/allocator/network"
	"github.com/docker/swarmkit/manager/allocator/network/errors"

	"github.com/docker/swarmkit/api"
	"github.com/docker/swarmkit/log"
	"github.com/docker/swarmkit/manager/state/store"
	"github.com/docker/swarmkit/protobuf/ptypes"
)

const (
	AllocatedStatusMessage = "pending task scheduling"
)

type NewAllocator struct {
	store   *store.MemoryStore
	network network.Allocator
}

func NewNew(store *store.MemoryStore, pg plugingetter.PluginGetter) *NewAllocator {
	a := &NewAllocator{
		store:   store,
		network: network.NewAllocator(pg),
	}
	return a
}

func (a *NewAllocator) Run(ctx context.Context) error {
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
	// deleted, the event we recieve is our last chance to deal with that
	// object. In that case, we immediately call into Deallocate.

	ctx, c := context.WithCancel(ctx)
	// defer canceling the context, so that anything waiting on it will exit
	// when this routine exits.
	defer c()
	ctx = log.WithModule(ctx, "allocator")

	// we want to spend as little time as possible in transactions, because
	// transactions stop the whole cluster, so we're going to grab the lists
	// and then get out
	var (
		networks []*api.Network
		services []*api.Service
		tasks    []*api.Task
	)
	watch, cancel, err := store.ViewAndWatch(a.store,
		func(tx store.ReadTx) error {
			var err error
			networks, err = store.FindNetworks(tx, store.All)
			if err != nil {
				// TODO(dperny): handle errors
			}
			services, err = store.FindServices(tx, store.All)
			if err != nil {
				// TODO(dperny): handle errors
			}
			tasks, err = store.FindTasks(tx, store.All)
			if err != nil {
				// TODO(dperny): handle errors
			}

			return nil
		},
		api.EventCreateNetwork{},
		api.EventUpdateNetwork{},
		api.EventDeleteNetwork{},
		api.EventCreateService{},
		api.EventUpdateService{},
		api.EventDeleteService{},
		api.EventCreateTask{},
		api.EventUpdateTask{},
		api.EventDeleteTask{},
		api.EventCreateNode{},
		api.EventUpdateNode{},
		api.EventDeleteNode{},
	)
	if err != nil {
		// TODO(dperny): error handling
		return err
	}

	// set up a routine to cancel the event stream when the context is canceled
	go func() {
		select {
		case <-ctx.Done():
			// cancel the event stream and wake all of the waiting goroutines
			cancel()
		}
	}()

	// now restore the local state
	if err := a.network.Restore(networks, services, tasks); err != nil {
		// TODO(dperny): handle errors
	}

	if err := a.store.Batch(func(batch *store.Batch) error {
		for _, network := range networks {
			batch.Update(func(tx store.Tx) error {
				nw := store.GetNetwork(tx, network.ID)
				if nw != nil {
					err := a.network.AllocateNetwork(nw)
					if errors.IsErrAlreadyAllocated(err) {
						return nil
					}
					return err
				}
				return nil
			})
		}
		for _, task := range tasks {
			if err := batch.Update(func(tx store.Tx) error {
				return a.allocateTask(tx, task)
			}); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		// TODO(dperny): error handling
		return err
	}

	// this goroutine handles incoming work.
	/*
		go func() {
			for {
				select {
				case <-ctx.Done():
					return
				case networkID := <-pendingNetworksOut:
					// keep track of the allocation error separately. if the
					// network allocator returns an error, it should not alter
					// state, and we can retry allocation. if there is a
					// transaction error, then we have some state allocated
					// locally, but synced up to the store, and we have to handle
					// that separately. There is no good way to roll back an
					// allocation
					var allocationErr error
					if err := a.store.Update(func(tx store.Tx) error {
						nw := store.GetNetwork(tx, networkID)
						if nw == nil {
							return nil
						}
						if err := a.network.AllocateNetwork(nw); err != nil {
							allocationErr = err
							return err
						}
					}); err != nil {
						// if allocationErr != nil {
							// if it's an allocation error, then we can safely just
							// add the object back to the work pool and try it
							// again later
						// } else {
							// otherwise, we need to set up some way of retrying
							// the transaction. chances are if the tx fails, it's
							// the end of our tenure as leader, but not always
							// TODO(dperny): do this
						// }
					}
				case taskID := <-pendingTasksOut:
					// tasks are more complicated than networks, because they have
					// a dependency on services and networks, which must be
					// allocated first. However, we can guarantee that task
					// dependencies are fully allocated by first allocating their
					// services and tasks.
					var (
						allocationErr error
					)
					// TODO(dperny): add some fancy intelligent logic for
					// allocating many tasks at once after a service update
					if err := a.store.Update(func(tx store.Tx) {
						task := store.GetTask(tx, taskID)
						// if the task is nil, it was probably deleted before we
						// serviced it, so we're done
						if task == nil {
							return nil
						}
						// deallocating the task
						if task.Status.State >= api.TaskStateCompleted {
							a.network.DeallocateTask(task)
							if err := store.UpdateTask(a, task); err != nil {
								return err
							}
							return nil
						}
						// check if we're allocating new tasks
						if task.Status.State == api.TaskStateNew && task.DesiredState == api.TaskStateRunning {
							// if so, we're gonna allocate first the service
							service := store.GetService(tx, task.ServiceID)
							if service != nil {
								serviceCopy := service.Copy()
								if err := a.network.AllocateService(service); err != nil {
									allocationErr = err
									return err
								}
								// no need to commit a change if nothing changed
								// TODO(dperny): maybe it's better to return
								// something like an errAlreadyAllocated and check
								// that, to see if no work is done...
								if !reflect.DeepEqual(service, serviceCopy) {
									if err := store.UpdateService(tx, service); err != nil {
										return err
									}
								}
							}
							if err := a.network.AllocateTask(task); err != nil {
								allocationErr = err
								return err
							}
							// if allocation succeeded, advance the task state to
							// PENDING
							task.Status = api.TaskStatus{
								State:     api.TaskStatePending,
								Message:   AllocatedStatusMessage,
								Timestamp: ptypes.MustTimestampProto(time.Now()),
							}
							if err := store.UpdateTask(tx, task); err != nil {
								return err
							}
						}
					}); err != nil {
						if allocationErr != nil {
							select {
							case <-ctx.Done():
							case pendingTasksIn <- taskID:
							}
						} else {
							// TODO(dperny): handle transaction errors that aren't
							// allocation errors
						}
					}
				}
			}
		}()
	*/

	// this goroutine handles incoming store events. all we need from the
	// events is the ID of what has been updated. by the time we service the
	// allocation, the object may have changed, so we don't save any other
	// information. we'll get to it later.
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case event := <-watch:
				switch ev := event.(type) {
				case api.EventCreateNetwork, api.EventUpdateNetwork:
					// get the network
					var n *api.Network
					if e, ok := ev.(api.EventCreateNetwork); ok {
						n = e.Network
					} else {
						n = ev.(api.EventUpdateNetwork).Network
					}
					if n != nil {
						a.store.Update(func(tx store.Tx) error {
							if err := a.network.AllocateNetwork(n); err != nil {
								return err
							}
							return store.UpdateNetwork(tx, n)
						})
					}
				case api.EventDeleteNetwork:
					// if the user deletes  the network, we don't have to do any
					// store actions, and we can't return any errors. The network
					// is already gone, deal with it
					if ev.Network != nil {
						a.network.DeallocateNetwork(ev.Network)
					}
				case api.EventDeleteService:
					if ev.Service != nil {
						a.network.DeallocateService(ev.Service)
					}
				case api.EventCreateTask, api.EventUpdateTask:
					var t *api.Task
					if e, ok := ev.(api.EventCreateTask); ok {
						t = e.Task
					} else {
						t = ev.(api.EventUpdateTask).Task
					}
					if t != nil {
						if t.Status.State >= api.TaskStateCompleted {
							a.network.DeallocateTask(t)
							continue
						}
						a.store.Update(func(tx store.Tx) error {
							return a.allocateTask(tx, t)
						})

					}
				case api.EventDeleteTask:
					if ev.Task != nil {
						a.network.DeallocateTask(ev.Task)
					}
				}
			}
		}
	}()
	return nil
}

func (a *NewAllocator) allocateTask(tx store.Tx, t *api.Task) error {
	task := store.GetTask(tx, t.ID)
	if task == nil {
		return nil
	}
	if task.ServiceID != "" {
		service := store.GetService(tx, task.ServiceID)
		// nothing to do, task will be deleted soon
		if service == nil {
			return nil
		}
		err := a.network.AllocateService(service)
		if err != nil && !errors.IsErrAlreadyAllocated(err) {
			return err
		}
		if err == nil {
			store.UpdateService(tx, service)
		}
	}
	err := a.network.AllocateTask(task)
	if errors.IsErrAlreadyAllocated(err) {
		return nil
	}
	if err != nil {
		return err
	}
	task.Status = api.TaskStatus{
		State:     api.TaskStatePending,
		Message:   AllocatedStatusMessage,
		Timestamp: ptypes.MustTimestampProto(time.Now()),
	}
	return store.UpdateTask(tx, task)
}
