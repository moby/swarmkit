package agent

import (
	"sync"

	"github.com/boltdb/bolt"
	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/log"
	"golang.org/x/net/context"
)

type Worker interface {
	Assign(ctx context.Context, tasks []*api.Task) error
}

type worker struct {
	db       *bolt.DB
	factory  TaskManagerFactory
	reporter StatusReporter
	tasks    map[string]TaskManager
	mu       sync.RWMutex
}

func newWorker(ctx context.Context, db *bolt.DB, factory TaskManagerFactory, reporter StatusReporter) *worker {
	reporter = statusReporterFunc(func(ctx context.Context, taskID string, status *api.TaskStatus) error {
		if err := db.Update(func(tx *bolt.Tx) error {
			return PutTaskStatus(tx, taskID, status)
		}); err != nil {
			log.G(ctx).WithError(err).Error("failed writing status to disk")
		}

		return reporter.UpdateTaskStatus(ctx, taskID, status)
	})

	taskManagers := make(map[string]TaskManager)
	// read the tasks from the database and start any task managers that may be needed.
	if err := db.Update(func(tx *bolt.Tx) error {
		for _, task := range GetTasks(tx) {
			if !TaskAssigned(tx, task.ID) {
				// NOTE(stevvooe): If tasks can survive worker restart, we need
				// to startup the controller and ensure they are removed. For
				// now, we can simply remove them from the database.
				if err := DeleteTask(tx, task.ID); err != nil {
					log.G(ctx).WithError(err).Errorf("error removing task %v", task.ID)
				}
				continue
			}

			status, err := GetTaskStatus(tx, task.ID)
			if err != nil {
				log.G(ctx).WithError(err).Error("unable to read tasks status")
				continue
			}

			task.Status = *status // merges the status into the task, ensuring we start at the right point.
			taskManagers[task.ID] = factory.TaskManager(ctx, task, reporter)
		}

		return nil
	}); err != nil {
		log.G(ctx).WithError(err).Errorf("error resolving tasks on startup")
	}

	// TODO(stevvooe): Start task cleanup process.

	return &worker{
		db:       db,
		factory:  factory,
		reporter: reporter,
		tasks:    taskManagers,
	}
}

// Assign the set of tasks to the worker. Any tasks not previously known will
// be started. Any tasks that are in the task set and already running will be
// updated, if possible. Any tasks currently running on the
// worker outside the task set will be terminated.
func (w *worker) Assign(ctx context.Context, tasks []*api.Task) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	tx, err := w.db.Begin(true)
	if err != nil {
		log.G(ctx).WithError(err).Error("failed starting transaction against task database")
		return err
	}
	defer tx.Rollback()

	assigned := map[string]struct{}{}
	for _, task := range tasks {
		log.G(ctx).WithField("task.id", task.ID).Debug("assigned")
		if err := PutTask(tx, task); err != nil {
			return err
		}

		if err := SetTaskAssignment(tx, task.ID, true); err != nil {
			return err
		}

		if mgr, ok := w.tasks[task.ID]; ok {
			if err := mgr.Update(ctx, task); err != nil {
				log.G(ctx).WithError(err).Error("failed updating assigned task")
			}
		} else {
			// we may have still seen the task, let's grab the status from
			// storage and replace it with our status, if we have it.
			status, err := GetTaskStatus(tx, task.ID)
			if err != nil {
				if err != errTaskUnknown {
					return err
				}

				// never seen before, register the provided status
				if err := PutTaskStatus(tx, task.ID, &task.Status); err != nil {
					return err
				}

				status = &task.Status
			} else {
				task.Status = *status // overwrite the stale manager status with ours.
			}

			ctx := log.WithLogger(ctx, log.G(ctx).WithField("task.id", task.ID))
			w.tasks[task.ID] = w.factory.TaskManager(ctx, task, w.reporter)
		}

		assigned[task.ID] = struct{}{}
	}

	for id, task := range w.tasks {
		if _, ok := assigned[id]; ok {
			continue
		}

		ctx := log.WithLogger(ctx, log.G(ctx).WithField("task.id", id))

		// when a task is no longer assigned, we shutdown the task manager for
		// it and leave cleanup to the sweeper.
		if err := task.Close(); err != nil {
			log.G(ctx).WithError(err).Error("error closing task manager")
		}

		delete(w.tasks, id)
	}

	return tx.Commit()
}
