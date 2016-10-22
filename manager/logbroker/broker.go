package logbroker

import (
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"

	"github.com/Sirupsen/logrus"
	"github.com/docker/go-events"
	"github.com/docker/swarmkit/api"
	"github.com/docker/swarmkit/ca"
	"github.com/docker/swarmkit/identity"
	"github.com/docker/swarmkit/log"
	"github.com/docker/swarmkit/manager/state/watch"
	"golang.org/x/net/context"
)

// LogBroker coordinates log subscriptions to services and tasks. Çlients can
// publish and subscribe to logs channels.
//
// Log subscriptions are pushed to the work nodes by creating log subscsription
// tasks. As such, the LogBroker also acts as an orchestrator of these tasks.
type LogBroker struct {
	broadcaster   *watch.Queue
	subscriptions *watch.Queue

	stopped chan struct{}
}

// New initializes and returns a new LogBroker
func New() *LogBroker {
	return &LogBroker{
		broadcaster:   watch.NewQueue(),
		subscriptions: watch.NewQueue(),
		stopped:       make(chan struct{}),
	}
}

// Stop stops the log broker
func (lb *LogBroker) Stop() {
	close(lb.stopped)
	lb.broadcaster.Close()
	lb.subscriptions.Close()
}

func validateSelector(selector *api.LogSelector) error {
	if selector == nil {
		return grpc.Errorf(codes.InvalidArgument, "log selector must be provided")
	}

	if len(selector.ServiceIDs) == 0 && len(selector.TaskIDs) == 0 && len(selector.NodeIDs) == 0 {
		return grpc.Errorf(codes.InvalidArgument, "log selector must not be empty")
	}

	return nil
}

// SubscribeLogs creates a log subscription and streams back logs
func (lb *LogBroker) SubscribeLogs(request *api.SubscribeLogsRequest, stream api.Logs_SubscribeLogsServer) error {
	ctx := stream.Context()

	if err := validateSelector(request.Selector); err != nil {
		return err
	}

	subscription := &api.SubscriptionMessage{
		ID:       identity.NewID(),
		Selector: request.Selector,
		Options:  request.Options,
	}

	log := log.G(ctx).WithFields(
		logrus.Fields{
			"method":          "(*LogBroker).SubscribeLogs",
			"subscription.id": subscription.ID,
		},
	)

	publishCh, publishCancel := lb.broadcaster.CallbackWatch(events.MatcherFunc(func(event events.Event) bool {
		publish := event.(*api.PublishLogsRequest)
		return publish.SubscriptionID == subscription.ID
	}))
	defer publishCancel()

	lb.subscriptions.Publish(subscription)
	defer func() {
		log.Debug("closing subscription")
		subscription = subscription.Copy()
		subscription.Close = true
		lb.subscriptions.Publish(subscription)
	}()

	for {
		select {
		case event := <-publishCh:
			publish := event.(*api.PublishLogsRequest)
			if publish.Close {
				// TODO(aluzzardi): This is broken - we shouldn't stop just because
				// we received one Close - we should wait for all publishers to Close.
				if request.Options != nil && !request.Options.Follow {
					return nil
				}
				continue
			}
			if err := stream.Send(&api.SubscribeLogsMessage{
				Messages: publish.Messages,
			}); err != nil {
				return err
			}
		case <-ctx.Done():
			return ctx.Err()
		case <-lb.stopped:
			return nil
		}
	}
}

// ListenSubscriptions returns a stream of matching subscriptions for the current node
func (lb *LogBroker) ListenSubscriptions(request *api.ListenSubscriptionsRequest, stream api.LogBroker_ListenSubscriptionsServer) error {
	remote, err := ca.RemoteNode(stream.Context())
	if err != nil {
		return err
	}

	log := log.G(stream.Context()).WithFields(
		logrus.Fields{
			"method": "(*LogBroker).ListenSubscriptions",
			"node":   remote.NodeID,
		},
	)
	subscriptionCh, subscriptionCancel := lb.subscriptions.Watch()
	defer subscriptionCancel()

	log.Debug("node registered")
	for {
		select {
		case v := <-subscriptionCh:
			subscription := v.(*api.SubscriptionMessage)
			if err := stream.Send(subscription); err != nil {
				log.Error(err)
				return err
			}
		case <-stream.Context().Done():
			return stream.Context().Err()
		case <-lb.stopped:
			return nil
		}
	}
}

// PublishLogs publishes log messages for a given subscription
func (lb *LogBroker) PublishLogs(ctx context.Context, request *api.PublishLogsRequest) (*api.PublishLogsResponse, error) {
	if request.SubscriptionID == "" {
		return nil, grpc.Errorf(codes.InvalidArgument, "missing subscription ID")
	}
	lb.broadcaster.Publish(request)
	return &api.PublishLogsResponse{}, nil
}
