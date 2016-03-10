package agent

import (
	"time"

	"github.com/Sirupsen/logrus"
	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/log"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
)

// Agent implements the primary node functionality for a member of a swarm
// cluster. The primary functionality id to run and report on the status of
// tasks assigned to the node.
type Agent struct {
	config *Config
	err    error
	closed chan struct{}
	conn   *grpc.ClientConn
	picker *picker
	logger *logrus.Entry
}

// New returns a new agent, ready for task dispatch.
func New(config *Config) (*Agent, error) {
	if err := config.validate(); err != nil {
		return nil, err
	}

	return &Agent{
		config: config,
		logger: log.L.WithFields(logrus.Fields{
			"agent.id":   config.ID,
			"agent.name": config.Name,
		}),
	}, nil
}

// Run blocks forever, received tasks from a dispatcher.
func (a *Agent) Run(ctx context.Context) error {
	a.logger.Debugf("(*Agent).Run")
	ctx = log.WithLogger(ctx, a.logger)
	if err := a.connect(ctx); err != nil {
		return err
	}

	for {
		if err := a.session(ctx); err != nil {
			log.G(ctx).WithField("err", err).Errorf("session error")
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

	a.picker = newPicker(manager, a.config.Managers)
	a.conn, err = grpc.Dial(manager,
		grpc.WithPicker(a.picker),
		grpc.WithInsecure())
	if err != nil {
		return err
	}

	return err
}

// session starts the registration and heartbeat control cycle. Any failure
// will result in starting from registration and re-establishing heartbeat and
// session.
//
// All communication with the master is done through session.  Changes that
// flow into the agent, such as task assignment, are called back into the
// agent.
func (a *Agent) session(ctx context.Context) error {
	log.G(ctx).Debugf("(*Agent).session")
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	sessionID, err := a.register(ctx)
	if err != nil {
		return err
	}

	ctx = log.WithLogger(ctx, log.G(ctx).WithField("session.id", sessionID))

	var (
		errs    = make(chan error)
		session = make(chan *api.SessionMessage)
		tasks   = make(chan *api.TasksMessage)
	)

	// TODO(stevvooe): Kick off all asychronous goroutine tasks for servicing
	// the connection.  The goal here is to route all messages back into the
	// select statement below. When we wake up, we call back into the notified
	// compoent, be it the agent or something else.
	go a.heartbeat(ctx, sessionID, errs)
	go a.watch(ctx, sessionID, tasks, errs)
	go a.listen(ctx, sessionID, session, errs)

	for {
		select {
		case msg := <-session:
			seen := map[string]struct{}{}
			for _, manager := range msg.Managers {
				if manager.Addr == "" {
					log.G(ctx).WithField("manager.addr", manager.Addr).
						Warnf("skipping bad manager address")
					continue
				}

				a.config.Managers.Observe(manager.Addr, manager.Weight)
				seen[manager.Addr] = struct{}{}
			}

			if msg.Disconnect {
				if err := a.picker.Reset(); err != nil {
					return err
				}
			}

			// TODO(stevvooe): Right now, this deletes all the command line
			// entered managers, which stinks for working in development.

			// prune managers not in list.
			// known := a.config.Managers.All()
			// for _, addr := range known {
			// 	if _, ok := seen[addr]; !ok {
			// 		a.config.Managers.Remove(addr)
			// 	}
			// }
		case msg := <-tasks:
			if err := a.Assign(msg.Tasks); err != nil {
				return err
			}
		case err := <-errs:
			return err
		case <-ctx.Done():
		}
	}
}

func (a *Agent) register(ctx context.Context) (string, error) {
	log.G(ctx).Debugf("(*Agent).register")
	client := api.NewDispatcherClient(a.conn)

	resp, err := client.Register(ctx, &api.RegisterRequest{
		NodeID: a.config.ID,
		Spec: &api.NodeSpec{
			Meta: &api.Meta{Name: a.config.Name},
		},
	})
	if err != nil {
		if grpc.Code(err) == codes.NotFound {
			return "", errNodeNotRegistered
		}

		return "", err
	}

	return resp.SessionID, nil
}

func (a *Agent) heartbeat(ctx context.Context, sessionID string, errs chan error) {
	client := api.NewDispatcherClient(a.conn)
	heartbeat := time.NewTimer(1) // send out a heartbeat right away
	defer heartbeat.Stop()
	for {
		select {
		case <-heartbeat.C:
			start := time.Now()
			resp, err := client.Heartbeat(ctx, &api.HeartbeatRequest{
				NodeID:    a.config.ID,
				SessionID: sessionID,
			})
			if err != nil {
				if grpc.Code(err) == codes.NotFound {
					err = errNodeNotRegistered
				}

				select {
				case errs <- err:
				case <-ctx.Done():
				}

				return
			}

			heartbeat.Reset(resp.Period)
			log.G(ctx).WithFields(
				logrus.Fields{
					"period":        resp.Period,
					"grpc.duration": time.Since(start),
				}).Debugf("heartbeat")
		case <-ctx.Done():
			return
		}
	}
}

func (a *Agent) listen(ctx context.Context, sessionID string, msgs chan *api.SessionMessage, errs chan error) {
	log.G(ctx).Debugf("(*Agent).listen")
	client := api.NewDispatcherClient(a.conn)
	session, err := client.Session(ctx, &api.SessionRequest{
		NodeID:    a.config.ID,
		SessionID: sessionID,
	})
	if err != nil {
		select {
		case errs <- err:
		case <-ctx.Done():
		}
		return
	}

	for {
		resp, err := session.Recv()
		if err != nil {
			select {
			case errs <- err:
			case <-ctx.Done():
			}
			return
		}

		select {
		case msgs <- resp:
		case <-ctx.Done():
			return
		}
	}
}

func (a *Agent) watch(ctx context.Context, sessionID string, msgs chan *api.TasksMessage, errs chan error) {
	log.G(ctx).Debugf("(*Agent).watch")
	client := api.NewDispatcherClient(a.conn)
	watch, err := client.Tasks(ctx, &api.TasksRequest{NodeID: a.config.ID, SessionID: sessionID})
	if err != nil {
		select {
		case errs <- err:
		case <-ctx.Done():
		}
		return
	}

	for {
		resp, err := watch.Recv()
		if err != nil {
			select {
			case errs <- err:
			case <-ctx.Done():
			}
			return

		}

		select {
		case msgs <- resp:
		case <-ctx.Done():
			return
		}
	}
}

// Assign the set of tasks to the agent. Any tasks on the agent currently that
// are not in the provided set will be terminated.
func (a *Agent) Assign(tasks []*api.Task) error {
	a.logger.WithField("tasks", tasks).Debugf("(*Agent).Assign")
	return nil
}
