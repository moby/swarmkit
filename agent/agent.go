package agent

import (
	"errors"
	"math/rand"
	"reflect"
	"time"

	"github.com/Sirupsen/logrus"
	"github.com/docker/swarm-v2/agent/exec"
	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/log"
	"github.com/docker/swarm-v2/protobuf/ptypes"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
)

const (
	initialSessionFailureBackoff = time.Second
	maxSessionFailureBackoff     = 8 * time.Second
)

// Agent implements the primary node functionality for a member of a swarm
// cluster. The primary functionality id to run and report on the status of
// tasks assigned to the node.
type Agent struct {
	config *Config
	conn   *grpc.ClientConn
	picker *picker

	tasks       map[string]*api.Task // contains all managed tasks
	assigned    map[string]*api.Task // contains current assignment set
	statuses    map[string]*api.TaskStatus
	controllers map[string]exec.Runner // contains all runners

	statusq chan taskStatusReport

	started chan struct{}
	stopped chan struct{} // requests shutdown
	closed  chan struct{} // only closed in run
	err     error         // read only after closed is closed
}

// New returns a new agent, ready for task dispatch.
func New(config *Config) (*Agent, error) {
	if err := config.validate(); err != nil {
		return nil, err
	}

	return &Agent{
		config:      config,
		tasks:       make(map[string]*api.Task),
		assigned:    make(map[string]*api.Task),
		statuses:    make(map[string]*api.TaskStatus),
		controllers: make(map[string]exec.Runner),

		statusq: make(chan taskStatusReport),

		started: make(chan struct{}),
		stopped: make(chan struct{}),
		closed:  make(chan struct{}),
	}, nil
}

var (
	errAgentNotStarted = errors.New("agent: not started")
	errAgentStarted    = errors.New("agent: already started")
	errAgentStopped    = errors.New("agent: stopped")

	errTaskNoContoller            = errors.New("agent: no task controller")
	errTaskNotAssigned            = errors.New("agent: task not assigned")
	errTaskInvalidStateTransition = errors.New("agent: invalid task transition")
	errTaskStatusUpdateNoChange   = errors.New("agent: no change in task status")
	errTaskDead                   = errors.New("agent: task dead")
	errTaskUnknown                = errors.New("agent: task unknown")
)

// Start begins execution of the agent in the provided context, if not already
// started.
func (a *Agent) Start(ctx context.Context) error {
	select {
	case <-a.started:
		select {
		case <-a.closed:
			return a.err
		case <-a.stopped:
			return errAgentStopped
		case <-ctx.Done():
			return ctx.Err()
		default:
			return errAgentStarted
		}
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	close(a.started)
	go a.run(ctx)

	return nil
}

// Stop shuts down the agent, blocking until full shutdown. If the agent is not
// started, Stop will block until Started.
func (a *Agent) Stop(ctx context.Context) error {
	select {
	case <-a.started:
		select {
		case <-a.closed:
			return a.err
		case <-a.stopped:
			select {
			case <-a.closed:
				return a.err
			case <-ctx.Done():
				return ctx.Err()
			}
		case <-ctx.Done():
			return ctx.Err()
		default:
			close(a.stopped)
			// recurse and wait for closure
			return a.Stop(ctx)
		}
	case <-ctx.Done():
		return ctx.Err()
	default:
		return errAgentNotStarted
	}
}

// Err returns the error that caused the agent to shutdown or nil. Err blocks
// until the agent is fully shutdown.
func (a *Agent) Err() error {
	select {
	case <-a.closed:
		return a.err
	}
}

func (a *Agent) run(ctx context.Context) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	ctx = log.WithLogger(ctx, log.G(ctx).WithFields(logrus.Fields{
		"agent.id": a.config.ID,
	}))

	log.G(ctx).Debugf("(*Agent).run")
	defer log.G(ctx).Debugf("(*Agent).run exited")
	defer close(a.closed) // full shutdown.

	if err := a.connect(ctx); err != nil {
		log.G(ctx).WithError(err).Error("agent: connection failed")
		a.err = err
		return
	}

	var (
		backoff    time.Duration
		session    = newSession(ctx, a, backoff) // start the initial session
		registered = session.registered
	)

	// TODO(stevvooe): Read tasks known by executor associated with this node
	// and begin to manage them. This may be as simple as reporting their run
	// status and waiting for instruction from the manager.

	// TODO(stevvoe): Read tasks from disk store.

	for {
		select {
		case report := <-a.statusq:
			if err := a.handleTaskStatusReport(ctx, session, report); err != nil {
				log.G(ctx).WithError(err).Error("task status report handler failed")
			}
		case msg := <-session.tasks:
			if err := a.handleTaskAssignment(ctx, msg.Tasks); err != nil {
				log.G(ctx).WithError(err).Error("task assignment failed")
			}
		case msg := <-session.messages:
			if err := a.handleSessionMessage(ctx, msg); err != nil {
				log.G(ctx).WithError(err).Error("session message handler failed")
			}
		case <-registered:
			log.G(ctx).Debugln("agent: registered")
			registered = nil // we only care about this once per session
			backoff = 0      // reset backoff
		case err := <-session.errs:
			// TODO(stevvooe): This may actually block if a session is closed
			// but no error was sent. Session.close must only be called here
			// for this to work.
			if err != nil {
				log.G(ctx).WithError(err).Error("agent: session failed")
				backoff = initialSessionFailureBackoff + 2*backoff
				if backoff > maxSessionFailureBackoff {
					backoff = maxSessionFailureBackoff
				}
			}

			if err := session.close(); err != nil {
				log.G(ctx).WithError(err).Error("agent: closing session failed")
			}
		case <-session.closed:
			log.G(ctx).Debugf("agent: rebuild session")

			// select a session registration delay from backoff range.
			delay := time.Duration(rand.Int63n(int64(backoff)))
			session = newSession(ctx, a, delay)
			registered = session.registered
		case <-a.stopped:
			// TODO(stevvooe): Wait on shutdown and cleanup. May need to pump
			// this loop a few times.
			return
		case <-ctx.Done():
			if a.err == nil {
				a.err = ctx.Err()
			}

			return
		}
	}
}

// connect creates the client connection. This should only be called once per
// agent.
func (a *Agent) connect(ctx context.Context) error {
	log.G(ctx).Debugf("(*Agent).connect")

	manager, err := a.config.Managers.Select()
	if err != nil {
		return err
	}

	backoff := *grpc.DefaultBackoffConfig
	backoff.MaxDelay = maxSessionFailureBackoff

	creds := a.config.SecurityConfig.ClientTLSCreds
	a.picker = newPicker(manager, a.config.Managers)
	a.conn, err = grpc.Dial(manager,
		grpc.WithPicker(a.picker),
		grpc.WithTransportCredentials(creds),
		grpc.WithBackoffConfig(&backoff))
	if err != nil {
		return err
	}

	return err
}

func (a *Agent) handleSessionMessage(ctx context.Context, message *api.SessionMessage) error {
	seen := map[string]struct{}{}
	for _, manager := range message.Managers {
		if manager.Addr == "" {
			log.G(ctx).WithField("manager.addr", manager.Addr).
				Warnf("skipping bad manager address")
			continue
		}

		a.config.Managers.Observe(manager.Addr, int(manager.Weight))
		seen[manager.Addr] = struct{}{}
	}

	if message.Disconnect {
		// TODO(stevvooe): This may actually be fatal if there is a failure.
		return a.picker.Reset()
	}

	return nil

	// TODO(stevvooe): Right now, this deletes all the command line
	// entered managers, which stinks for working in development.

	// prune managers not in list.
	// known := a.config.Managers.All()
	// for _, addr := range known {
	// 	if _, ok := seen[addr]; !ok {
	// 		a.config.Managers.Remove(addr)
	// 	}
	// }

}

// assign the set of tasks to the agent. Any tasks on the agent currently that
// are not in the provided set will be terminated.
//
// This method run synchronously in the main session loop. It has direct access
// to fields and datastructures but must not block.
func (a *Agent) handleTaskAssignment(ctx context.Context, tasks []*api.Task) error {
	log.G(ctx).Debugf("(*Agent).handleTaskAssignment")

	assigned := map[string]*api.Task{}
	for _, task := range tasks {
		if task.DesiredState > api.TaskStateRunning {
			// Skip tasks which the manager wants to be stopped.
			continue
		}
		assigned[task.ID] = task
		ctx := log.WithLogger(ctx, log.G(ctx).WithField("task.id", task.ID))

		if _, ok := a.controllers[task.ID]; ok {
			if err := a.updateTask(ctx, task); err != nil {
				log.G(ctx).WithError(err).Error("task update failed")
			}
			continue
		}
		log.G(ctx).Debugf("assigned")
		if err := a.acceptTask(ctx, task); err != nil {
			log.G(ctx).WithError(err).Error("starting task controller failed")
			go func() {
				if err := a.report(ctx, task.ID, api.TaskStateRejected, "rejected task during assignment", err); err != nil {
					log.G(ctx).WithError(err).Error("reporting task rejection failed")
				}
			}()
		}
	}

	for id, task := range a.tasks {
		if _, ok := assigned[id]; ok {
			continue
		}
		delete(a.assigned, id)

		ctx := log.WithLogger(ctx, log.G(ctx).WithField("task.id", id))

		// if the task is already in finalize state, no need to call removeTask.
		if a.statuses[task.ID].State >= api.TaskStateFinalize {
			continue
		}

		// don't remove the task if the manager doesn't want us to.
		if task.DesiredState < api.TaskStateDead {
			continue
		}

		// TODO(stevvooe): Modify this to take the task through a graceful
		// shutdown. This just outright removes it.
		if err := a.removeTask(ctx, task); err != nil {
			log.G(ctx).WithError(err).Error("removing task failed")
		}
	}

	return nil
}

func (a *Agent) handleTaskStatusReport(ctx context.Context, session *session, report taskStatusReport) error {
	var respErr error
	err := a.updateStatus(ctx, report)
	if err == errTaskUnknown || err == errTaskDead || err == errTaskStatusUpdateNoChange {
		respErr = nil
	}

	if report.response != nil {
		// this channel is always buffered.
		report.response <- respErr
		report.response = nil // clear response channel
	}

	if err != nil {
		return respErr
	}

	// TODO(stevvooe): Coalesce status updates.
	go func() {
		if err := session.sendTaskStatus(ctx, report.taskID, a.statuses[report.taskID]); err != nil {
			log.G(ctx).WithError(err).Error("sending task status update failed")

			time.Sleep(time.Second) // backoff for retry
			select {
			case a.statusq <- report: // queue for retry
			case <-a.closed:
			case <-ctx.Done():
			}
		}
	}()

	return nil
}

func (a *Agent) updateStatus(ctx context.Context, report taskStatusReport) error {
	ctx = log.WithLogger(ctx, log.G(ctx).WithField("task.id", report.taskID))

	status, ok := a.statuses[report.taskID]
	if !ok {
		return errTaskUnknown
	}
	task, ok := a.tasks[report.taskID]
	if !ok {
		return errTaskUnknown
	}

	original := status.Copy()

	// validate transition only moves forward or updates fields
	if report.state < status.State && report.err == nil {
		log.G(ctx).Errorf("%v -> %v invalid!", status.State, report.state)
		return errTaskInvalidStateTransition
	}

	if report.err != nil {
		// If the task has been started, we return fail on error. If it has
		// not, we return rejected. While we don't do much differently for each
		// error type, it tells us the stage in which an error was encountered.
		switch status.State {
		case api.TaskStateNew, api.TaskStateAllocated,
			api.TaskStateAssigned, api.TaskStateAccepted,
			api.TaskStatePreparing:
			status.State = api.TaskStateRejected
			status.TerminalState = api.TaskStateRejected
			status.Err = report.err.Error()
		case api.TaskStateReady, api.TaskStateStarting,
			api.TaskStateRunning, api.TaskStateShutdown:
			status.State = api.TaskStateFailed
			status.TerminalState = api.TaskStateFailed
			status.Err = report.err.Error()
		case api.TaskStateCompleted, api.TaskStateFailed,
			api.TaskStateRejected, api.TaskStateDead:
			// noop when we get an error in these states
		case api.TaskStateFinalize:
			if task.DesiredState >= api.TaskStateDead {
				if err := a.removeTask(ctx, task.Copy()); err != nil {
					log.G(ctx).WithError(err).Error("failed retrying remove task")
				}
			}
		}
	} else {
		status.State = report.state
		switch report.state {
		case api.TaskStateRejected, api.TaskStateFailed, api.TaskStateCompleted:
			status.TerminalState = report.state
		}
	}

	tsp, err := ptypes.TimestampProto(report.timestamp)
	if err != nil {
		return err
	}

	status.Timestamp = tsp
	status.Message = report.message

	if reflect.DeepEqual(status, original) {
		return errTaskStatusUpdateNoChange
	}

	log.G(ctx).WithFields(logrus.Fields{
		"state.from":    original.State,
		"state.to":      status.State,
		"state.message": status.Message,
	}).Infof("task status updated")

	switch status.State {
	case api.TaskStateNew, api.TaskStateAllocated,
		api.TaskStateAssigned, api.TaskStateAccepted,
		api.TaskStatePreparing, api.TaskStateReady,
		api.TaskStateStarting, api.TaskStateRunning,
		api.TaskStateShutdown, api.TaskStateCompleted,
		api.TaskStateFailed, api.TaskStateRejected,
		api.TaskStateFinalize:
		// TODO(stevvooe): This switch is laid out here to support actions
		// based on state transition. Each state below will include code that
		// is only run when transitioning into a task state for the first time.
	case api.TaskStateDead:
		// once a task is dead, we remove all resources associated with it.
		delete(a.controllers, report.taskID)
		delete(a.tasks, report.taskID)
		delete(a.statuses, report.taskID)

		return errTaskDead
	}

	return nil
}

func (a *Agent) acceptTask(ctx context.Context, task *api.Task) error {
	a.tasks[task.ID] = task
	a.assigned[task.ID] = task
	a.statuses[task.ID] = task.Status
	task.Status = nil

	runner, err := a.config.Executor.Runner(task.Copy())
	if err != nil {
		log.G(ctx).WithError(err).Error("runner resolution failed")
		return err
	}

	a.controllers[task.ID] = runner
	reporter := a.reporter(ctx, task)
	taskID := task.ID

	go func() {
		if err := reporter.Report(ctx, api.TaskStateAccepted, "accepted"); err != nil {
			// TODO(stevvooe): What to do here? should be a rare error or never happen
			log.G(ctx).WithError(err).Error("reporting accepted status")
			return
		}

		if task.DesiredState < api.TaskStateRunning {
			log.G(ctx).Error("accepting a task with a desired state below RUNNING is not yet implemented")
			return
		}

		if err := exec.Run(ctx, runner, reporter); err != nil {
			log.G(ctx).WithError(err).Error("task run failed")
			if err := a.report(ctx, taskID, api.TaskStateFailed, "execution failed", err); err != nil {
				log.G(ctx).WithError(err).Error("reporting task run error failed")
			}
			return
		}
	}()

	return nil
}

func (a *Agent) updateTask(ctx context.Context, t *api.Task) error {
	if _, ok := a.assigned[t.ID]; !ok {
		return errTaskNotAssigned
	}

	original := a.tasks[t.ID]
	t.Status = nil // clear this, since we keep it elsewhere to avoid overwrite.
	a.tasks[t.ID] = t
	a.assigned[t.ID] = t

	if !reflect.DeepEqual(t, original) {
		ctlr := a.controllers[t.ID]
		// propagate the update if there are actual changes
		go func() {
			if err := ctlr.Update(ctx, t.Copy()); err != nil {
				log.G(ctx).WithError(err).Error("propagating task update failed")
			}
		}()
	}

	return nil
}

func (a *Agent) removeTask(ctx context.Context, t *api.Task) error {
	log.G(ctx).Debugf("(*Agent).removeTask")

	var (
		ctlr   = a.controllers[t.ID]
		taskID = t.ID
	)

	go func() {
		if err := a.report(ctx, taskID, api.TaskStateFinalize, "removing"); err != nil {
			log.G(ctx).WithError(err).Error("failed to report finalization")
			return
		}

		if err := ctlr.Remove(ctx); err != nil {
			log.G(ctx).WithError(err).Error("remove failed")
			if err := a.report(ctx, taskID, api.TaskStateFinalize, "remove failed", err); err != nil {
				log.G(ctx).WithError(err).Error("report remove error failed")
				return
			}
		}

		if err := a.report(ctx, taskID, api.TaskStateDead, "finalized"); err != nil {
			log.G(ctx).WithError(err).Error("failed reporting finalization")
			return
		}
	}()

	return nil
}

type taskStatusReport struct {
	timestamp time.Time
	taskID    string
	state     api.TaskState
	message   string
	err       error
	response  chan error
}

func (a *Agent) report(ctx context.Context, taskID string, state api.TaskState, msg string, errs ...error) error {
	log.G(ctx).Debugf("(*Agent).report")
	if len(errs) > 1 {
		panic("only one error per report is allowed")
	}

	var err error
	if len(errs) == 1 {
		err = errs[0]
	}

	response := make(chan error, 1)

	select {
	case a.statusq <- taskStatusReport{
		timestamp: time.Now(),
		taskID:    taskID,
		state:     state,
		message:   msg,
		err:       err,
		response:  response,
	}:
		select {
		case err := <-response:
			return err
		case <-a.closed:
			return ErrAgentClosed
		case <-ctx.Done():
			return ctx.Err()
		}
	case <-a.closed:
		return ErrAgentClosed
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (a *Agent) reporter(ctx context.Context, t *api.Task) exec.Reporter {
	id := t.ID
	return reporterFunc(func(ctx context.Context, state api.TaskState, msg string) error {
		return a.report(ctx, id, state, msg)
	})
}

type reporterFunc func(ctx context.Context, state api.TaskState, msg string) error

func (fn reporterFunc) Report(ctx context.Context, state api.TaskState, msg string) error {
	return fn(ctx, state, msg)
}
