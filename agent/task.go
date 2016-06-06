package agent

import (
	"reflect"

	"github.com/docker/swarm-v2/agent/exec"
	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/log"
	"golang.org/x/net/context"
)

// taskManager manages all aspects of task execution and reporting for an agent
// through state management.
type taskManager struct {
	task     *api.Task
	ctlr     exec.Controller
	reporter StatusReporter

	updateq chan *api.Task

	shutdown chan struct{}
	closed   chan struct{}
}

func newTaskManager(ctx context.Context, task *api.Task, ctlr exec.Controller, reporter StatusReporter) *taskManager {
	t := &taskManager{
		task:     task.Copy(),
		ctlr:     ctlr,
		reporter: reporter,
		updateq:  make(chan *api.Task),
		shutdown: make(chan struct{}),
		closed:   make(chan struct{}),
	}
	go t.run(ctx)
	return t
}

// Update the task data.
func (tm *taskManager) Update(ctx context.Context, task *api.Task) error {
	select {
	case tm.updateq <- task:
		return nil
	case <-tm.closed:
		return ErrClosed
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Close shuts down the task manager, blocking until it is stopped.
func (tm *taskManager) Close() error {
	log.L.Debug("(*taskManager).Close")

	select {
	case <-tm.closed:
		return nil
	case <-tm.shutdown:
	default:
		close(tm.shutdown)
	}

	select {
	case <-tm.closed:
		return nil
	}
}

func (tm *taskManager) run(ctx context.Context) {
	ctx, cancelAll := context.WithCancel(ctx)
	defer cancelAll() // cancel all child operations on exit.

	var (
		opctx    context.Context
		cancel   context.CancelFunc
		run      = make(chan struct{}, 1)
		statusq  = make(chan *api.TaskStatus)
		errs     = make(chan error)
		shutdown = tm.shutdown
		updated  bool // true if the task was updated.
	)

	log.G(ctx).Debug("(*taskManager).run")

	defer func() {
		// closure  picks up current value of cancel.
		if cancel != nil {
			cancel()
		}
	}()

	run <- struct{}{} // prime the pump
	for {
		select {
		case <-run:
			// always check for shutdown before running.
			select {
			case <-shutdown:
				continue // ignore run request and handle shutdown
			case <-tm.closed:
				continue
			default:
			}

			opctx, cancel = context.WithCancel(ctx)
			opcancel := cancel        // fork for the closure
			running := tm.task.Copy() // clone the task before dispatch
			go runctx(ctx, tm.closed, errs, func(ctx context.Context) error {
				defer opcancel()

				if updated {
					// before we do anything, update the task for the controller.
					// always update the controller before running.
					if err := tm.ctlr.Update(opctx, running); err != nil {
						log.G(ctx).WithError(err).Error("updating task controller failed")
						return err
					}
					updated = false
				}

				status, err := exec.Do(opctx, running, tm.ctlr)
				if err != nil {
					return err
				}

				select {
				case statusq <- status:
				case <-ctx.Done(): // not opctx, since that may have been cancelled.
				}

				return nil
			})
		case task := <-tm.updateq:
			if tasksEqual(task, tm.task) {
				log.G(ctx).Debug("task update ignored")
				continue // ignore the update
			}

			if task.ID != tm.task.ID {
				log.G(ctx).WithField("task.update.id", task.ID).Error("received update for incorrect task")
				continue
			}

			if task.DesiredState < tm.task.DesiredState {
				log.G(ctx).WithField("task.update.desiredstate", task.DesiredState).
					Error("ignoring task update with invalid desired state")
				continue
			}

			// log.G(ctx).WithField("task.update", task).Debug("update accepted")

			task = task.Copy()
			task.Status = tm.task.Status // overwrite our status, as it is canonical.
			tm.task = task
			updated = true

			// we have accepted the task update
			if cancel != nil {
				cancel() // cancel outstanding if necessary.
			} else {
				// no outstanding operation, pump run queue
				run <- struct{}{}
			}
		case status := <-statusq:
			cancel = nil

			tm.task.Status = *status
			if err := tm.reporter.UpdateTaskStatus(ctx, tm.task.ID, tm.task.Status.Copy()); err != nil {
				log.G(ctx).WithError(err).Error("failed reporting status to agent")
			}

			run <- struct{}{} // pump the run queue!
		case err := <-errs:
			cancel = nil

			switch err {
			case nil:
				continue // success, status, will be hit
			case exec.ErrTaskNoop:
				continue // wait till getting pumped via update.
			case context.Canceled, context.DeadlineExceeded:
			default:
				log.G(ctx).WithError(err).Error("task operation failed")
			}

			run <- struct{}{}
		case <-shutdown:
			if cancel != nil {
				// cancel outstanding operation.
				cancel()

				// This ensures we wait for the last operation to complete.
				// errs or status will be pumped but not requeue an operation.
				continue
			}

			// disable everything, and prepare for closing.
			statusq = nil
			errs = nil
			shutdown = nil
			close(tm.closed)
		case <-tm.closed:
			return
		case <-ctx.Done():
			return
		}
	}
}

// tasksEqual returns true if the tasks are functionaly equal, ignoring status,
// version and other superfluous fields.
//
// This used to decide whether or not to propagate a task update to a controller.
func tasksEqual(a, b *api.Task) bool {
	a, b = a.Copy(), b.Copy()

	a.Status, b.Status = api.TaskStatus{}, api.TaskStatus{}
	a.Meta, b.Meta = api.Meta{}, api.Meta{}

	return reflect.DeepEqual(a, b)
}
