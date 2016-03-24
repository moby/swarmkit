package agent

import (
	"errors"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"

	"github.com/Sirupsen/logrus"
	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/log"
	"golang.org/x/net/context"
)

var (
	errSessionClosed = errors.New("agent: session closed")
)

// session encapsulates one round of registration with the manager. session
// starts the registration and heartbeat control cycle. Any failure will result
// in a complete shutdown of the session and it must be reestablished.
//
// All communication with the master is done through session.  Changes that
// flow into the agent, such as task assignment, are called back into the
// agent through errs, messages and tasks.
type session struct {
	agent     *Agent
	sessionID string
	errs      chan error
	messages  chan *api.SessionMessage
	tasks     chan *api.TasksMessage

	registered chan struct{} // closed registration
	closed     chan struct{}
}

func newSession(ctx context.Context, agent *Agent) *session {
	s := &session{
		agent:      agent,
		errs:       make(chan error),
		messages:   make(chan *api.SessionMessage),
		tasks:      make(chan *api.TasksMessage),
		registered: make(chan struct{}),
		closed:     make(chan struct{}),
	}

	go s.run(ctx)
	return s
}

func (s *session) run(ctx context.Context) {
	sessionID, err := s.register(ctx)
	if err != nil {
		select {
		case s.errs <- err:
		case <-s.closed:
		case <-ctx.Done():
		}
	}

	ctx = log.WithLogger(ctx, log.G(ctx).WithField("session.id", sessionID))
	s.sessionID = sessionID
	close(s.registered)

	go runctx(ctx, s.heartbeat, s.closed, s.errs)
	go runctx(ctx, s.watch, s.closed, s.errs)
	go runctx(ctx, s.listen, s.closed, s.errs)
}

func (s *session) register(ctx context.Context) (string, error) {
	log.G(ctx).Debugf("(*session).register")
	client := api.NewDispatcherClient(s.agent.conn)

	description, err := s.agent.config.Executor.Describe(ctx)
	if s.agent.config.Hostname != "" {
		description.Hostname = s.agent.config.Hostname
	}

	resp, err := client.Register(ctx, &api.RegisterRequest{
		NodeID:      s.agent.config.ID,
		Description: description,
	})
	if err != nil {
		if grpc.Code(err) == codes.NotFound {
			return "", errNodeNotRegistered
		}

		return "", err
	}

	return resp.SessionID, nil
}

func (s *session) heartbeat(ctx context.Context) error {
	log.G(ctx).Debugf("(*session).heartbeat")
	client := api.NewDispatcherClient(s.agent.conn)
	heartbeat := time.NewTimer(1) // send out a heartbeat right away
	defer heartbeat.Stop()

	for {
		select {
		case <-heartbeat.C:
			start := time.Now()
			resp, err := client.Heartbeat(ctx, &api.HeartbeatRequest{
				NodeID:    s.agent.config.ID,
				SessionID: s.sessionID,
			})
			if err != nil {
				if grpc.Code(err) == codes.NotFound {
					err = errNodeNotRegistered
				}

				return err
			}

			heartbeat.Reset(resp.Period)
			log.G(ctx).WithFields(
				logrus.Fields{
					"period":        resp.Period,
					"grpc.duration": time.Since(start),
				}).Debugf("heartbeat")
		case <-s.closed:
			return errSessionClosed
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (s *session) listen(ctx context.Context) error {
	log.G(ctx).Debugf("(*session).listen")
	client := api.NewDispatcherClient(s.agent.conn)
	session, err := client.Session(ctx, &api.SessionRequest{
		NodeID:    s.agent.config.ID,
		SessionID: s.sessionID,
	})
	if err != nil {
		return err
	}

	for {
		resp, err := session.Recv()
		if err != nil {
			return err
		}

		select {
		case s.messages <- resp:
		case <-s.closed:
			return errSessionClosed
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (s *session) watch(ctx context.Context) error {
	log.G(ctx).Debugf("(*session).watch")
	client := api.NewDispatcherClient(s.agent.conn)
	watch, err := client.Tasks(ctx, &api.TasksRequest{
		NodeID:    s.agent.config.ID,
		SessionID: s.sessionID})
	if err != nil {
		return err
	}

	for {
		resp, err := watch.Recv()
		if err != nil {
			return err
		}

		select {
		case s.tasks <- resp:
		case <-s.closed:
			return errSessionClosed
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// sendTaskStatus uses the current session to send the status of a single task.
func (s *session) sendTaskStatus(ctx context.Context, taskID string, status *api.TaskStatus) error {
	select {
	case <-s.registered:
		select {
		case <-s.closed:
			return errSessionClosed
		default:
		}
	case <-s.closed:
		return errSessionClosed
	case <-ctx.Done():
		return ctx.Err()
	}

	client := api.NewDispatcherClient(s.agent.conn)
	if _, err := client.UpdateTaskStatus(ctx, &api.UpdateTaskStatusRequest{
		NodeID:    s.agent.config.ID,
		SessionID: s.sessionID,
		Updates: []*api.UpdateTaskStatusRequest_TaskStatusUpdate{
			{
				TaskID: taskID,
				Status: status,
			},
		},
	}); err != nil {
		return err
	}

	return nil
}

func (s *session) close() error {
	select {
	case <-s.closed:
		return errSessionClosed
	default:
		close(s.closed)
		return nil
	}
}

// runctx blocks until the function exits, closed is closed, or the context is
// cancelled. Call as part os go statement.
func runctx(ctx context.Context, fn func(ctx context.Context) error, closed chan struct{}, errs chan error) {
	select {
	case errs <- fn(ctx):
	case <-closed:
	case <-ctx.Done():
	}
}
