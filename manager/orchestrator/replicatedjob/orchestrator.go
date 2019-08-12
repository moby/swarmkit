package job

import (
	"context"
	"fmt"
	"sync"

	"github.com/docker/swarmkit/api"
	"github.com/docker/swarmkit/manager/orchestrator"
	"github.com/docker/swarmkit/manager/state/store"
)

// Orchestrator run a reconciliation loop to create and destroy tasks necessary
// for replicated jobs.
type Orchestrator struct {
	// we need the store, of course, to do updates
	store *store.MemoryStore

	// a copy of the cluster is needed, because we need it when creating tasks
	// to set the default log driver
	cluster *api.Cluster

	// stopChan is a channel that is closed to signal the orchestrator to stop
	// running
	stopChan chan struct{}
	// stopOnce is used to ensure that stopChan can only be closed once, just
	// in case some freak accident causes subsequent calls to Stop.
	stopOnce sync.Once
	// doneChan is closed when the orchestrator actually stops running
	doneChan chan struct{}
}

func NewOrchestrator(store *store.MemoryStore) *Orchestrator {
	return &Orchestrator{
		store:    store,
		stopChan: make(chan struct{}),
		doneChan: make(chan struct{}),
	}
}

// Run runs the Orchestrator reconciliation loop. It takes a context as an
// argument, but canceling this context will not stop the routine; this context
// is only for passing in logging information. Call Stop to stop the
// Orchestrator
func (o *Orchestrator) Run(ctx context.Context) {
	// closing doneChan should be the absolute last thing that happens in this
	// method, and so should be the absolute first thing we defer.
	defer close(o.doneChan)

	// TODO(dperny): this will be a case in the main select loop, but for now
	// just block until stopChan is closed.
	<-o.stopChan
}

// reconcileService reconciles the replicated job service with the given ID by
// checking to see if new replicas should be created. reconcileService returns
// an error if there is some case prevent it from correctly reconciling the
// service.
func (o *Orchestrator) reconcileService(id string) error {
	var (
		service *api.Service
		tasks   []*api.Task
		viewErr error
	)
	// first, get the service and all of its tasks
	o.store.View(func(tx store.ReadTx) {
		service = store.GetService(tx, id)

		tasks, viewErr = store.FindTasks(tx, store.ByServiceID(id))
	})

	if service == nil {
		return nil
	}

	// errors during view should only happen in a few rather catastrophic
	// cases, but here it's not unreasonable to just return an error anyway.
	if viewErr != nil {
		return viewErr
	}

	// if this is the first iteration of the service, it may not yet have a
	// JobStatus, so we should create one if so. this won't actually be
	// committed, though.
	if service.JobStatus == nil {
		service.JobStatus = &api.JobStatus{}
	}

	// Jobs can be run in multiple iterations. The JobStatus of the service
	// indicates which Version of iteration we're on. We should only be looking
	// at tasks of the latest Version

	jobVersion := service.JobStatus.JobIteration.Index

	// now, check how many tasks we need and how many we have running. note
	// that some of these Running tasks may complete before we even finish this
	// code block, and so we might have to immediately re-enter reconciliation,
	// so this number is 100% definitive, but it is accurate for this
	// particular moment in time, and it won't result in us going OVER the
	// needed task count
	//
	// also, we don't care if tasks are failed here; that is, we make no
	// distinction between tasks that have Failed and tasks in any other
	// terminal non-Completed state.
	//
	// also also, for the math later, we need these values to be of type uint64.
	runningTasks := uint64(0)
	completeTasks := uint64(0)

	// slots keeps track of the slot numbers available for tasks. as long as
	// the orchestrator isn't broken, then this map will consist of the set of
	// all integers from 0 to (MaxConcurrent - 1), with a boolean indicating if
	// that slot is occupied. This slot handling code is much simpler than the
	// code for replicated services, because we don't need to worry about
	// restarts.
	slots := map[uint64]bool{}
	for _, task := range tasks {
		// we only care about tasks from this job iteration. tasks from the
		// previous job iteration are not important
		// TODO(dperny): we need to stop any running tasks from older job
		// iterations.
		if task.JobIteration != nil && task.JobIteration.Index == jobVersion {
			if task.Status.State == api.TaskStateCompleted {
				completeTasks++
			}

			// any tasks that are created and running but not yet terminal are
			// considered "running tasks" for our purpose, because they don't
			// need to be created.
			if task.Status.State <= api.TaskStateRunning && task.DesiredState == api.TaskStateCompleted {
				runningTasks++
				slots[task.Slot] = true
			}
		}
	}

	// now that we have our counts, we need to see how many new tasks to
	// create. this number can never exceed MaxConcurrent, but also should not
	// result in us exceeding TotalCompletions. first, get these numbers out of
	// the service spec.
	rj := service.Spec.GetReplicatedJob()

	// possibleNewTasks gives us the upper bound for how many tasks we'll
	// create. also, ugh, subtracting uints. there's no way this can ever go
	// wrong.
	possibleNewTasks := rj.MaxConcurrent - runningTasks

	// allowedNewTasks is how many tasks we could create, if there were no
	// restriction on maximum concurrency. This is the total number of tasks
	// we want completed, minus the tasks that are already completed, minus
	// the tasks that are in progress.
	//
	// seriously, ugh, subtracting unsigned ints. totally a fine and not at all
	// risky operation, with no possibility for catastrophe
	allowedNewTasks := rj.TotalCompletions - completeTasks - runningTasks

	// the lower number of allowedNewTasks and possibleNewTasks is how many we
	// can create. we'll just use an if statement instead of some fancy floor
	// function.
	actualNewTasks := allowedNewTasks
	if possibleNewTasks < allowedNewTasks {
		actualNewTasks = possibleNewTasks
	}

	// this check might seem odd, but it protects us from an underflow of the
	// above subtractions, which, again, is a totally impossible thing that can
	// never happen, ever, obviously.
	if actualNewTasks > rj.TotalCompletions {
		return fmt.Errorf(
			"uint64 underflow, we're not going to create %v tasks",
			actualNewTasks,
		)
	}

	// finally, we can create these tasks. do this in a batch operation, to
	// avoid exceeding transaction size limits
	err := o.store.Batch(func(batch *store.Batch) error {
		for i := uint64(0); i < actualNewTasks; i++ {
			if err := batch.Update(func(tx store.Tx) error {
				// find an unoccupied slot number that we can use, same as with
				// replicated services.
				var slot uint64
				// the total number of slots we can have for a job is equal to
				// the value of MaxConcurrent for the job iteration. This means
				// if we iterate over values from 0 to MaxConcurrent, we'll
				// find an available slot.
				for s := uint64(0); s < rj.MaxConcurrent; s++ {
					// when we're iterating through, if the service has slots
					// that haven't been used yet (for example, if this is the
					// first time we're running this iteration), then doing
					// a map lookup for the number will return the 0-value
					// (false) even if the number doesn't exist in the map.
					if !slots[s] {
						slot = s
						// once we've found a slot, mark it as occupied, so we
						// don't double assign in subsequent iterations.
						slots[slot] = true
						break
					}
				}

				task := orchestrator.NewTask(o.cluster, service, slot, "")
				// when we create the task, we also need to set the
				// JobIteration.
				task.JobIteration = &api.Version{Index: jobVersion}
				task.DesiredState = api.TaskStateCompleted

				// finally, create the task in the store.
				return store.CreateTask(tx, task)
			}); err != nil {
				return err
			}
		}
		return nil
	})

	return err
}

// Stop stops the Orchestrator
func (o *Orchestrator) Stop() {
	// close stopChan inside of the Once so that there can be no races
	// involving multiple attempts to close stopChan.
	o.stopOnce.Do(func() {
		close(o.stopChan)
	})
	// now, we wait for the Orchestrator to stop. this wait is unqualified; we
	// will not return until Orchestrator has stopped successfully.
	<-o.doneChan
}
