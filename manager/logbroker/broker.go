package logbroker

import (
	"sync"

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
	mu                sync.RWMutex
	logQueue          *watch.Queue
	subscriptionQueue *watch.Queue

	registeredSubscriptions map[string]*api.SubscriptionMessage

	stopped chan struct{}
}

// New initializes and returns a new LogBroker
func New() *LogBroker {
	return &LogBroker{
		logQueue:                watch.NewQueue(),
		subscriptionQueue:       watch.NewQueue(),
		registeredSubscriptions: make(map[string]*api.SubscriptionMessage),
		stopped:                 make(chan struct{}),
	}
}

// Stop stops the log broker
func (lb *LogBroker) Stop() {
	close(lb.stopped)
	lb.logQueue.Close()
	lb.subscriptionQueue.Close()
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

func (lb *LogBroker) registerSubscription(subscription *api.SubscriptionMessage) {
	lb.mu.Lock()
	defer lb.mu.Unlock()

	lb.registeredSubscriptions[subscription.ID] = subscription
	lb.subscriptionQueue.Publish(subscription)
}

func (lb *LogBroker) unregisterSubscription(subscription *api.SubscriptionMessage) {
	subscription = subscription.Copy()
	subscription.Close = true

	lb.mu.Lock()
	defer lb.mu.Unlock()

	delete(lb.registeredSubscriptions, subscription.ID)
	lb.subscriptionQueue.Publish(subscription)
}

func (lb *LogBroker) subscriptions() ([]*api.SubscriptionMessage, chan events.Event, func()) {
	lb.mu.RLock()
	defer lb.mu.RUnlock()

	subs := make([]*api.SubscriptionMessage, 0, len(lb.registeredSubscriptions))
	for _, sub := range lb.registeredSubscriptions {
		subs = append(subs, sub)
	}

	ch, cancel := lb.subscriptionQueue.Watch()
	return subs, ch, cancel
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

	log.Debug("subscribed")

	publishCh, publishCancel := lb.logQueue.CallbackWatch(events.MatcherFunc(func(event events.Event) bool {
		publish := event.(*api.PublishLogsRequest)
		return publish.SubscriptionID == subscription.ID
	}))
	defer publishCancel()

	lb.registerSubscription(subscription)
	defer lb.unregisterSubscription(subscription)

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
	subscriptions, subscriptionCh, subscriptionCancel := lb.subscriptions()
	defer subscriptionCancel()

	log.Debug("node registered")

	// Start by sending down all active subscriptions.
	for _, subscription := range subscriptions {
		select {
		case <-stream.Context().Done():
			return stream.Context().Err()
		case <-lb.stopped:
			return nil
		default:
		}

		if err := stream.Send(subscription); err != nil {
			log.Error(err)
			return err
		}
	}

	// Send down new subscriptions.
	// TODO(aluzzardi): We should filter by relevant tasks for this node rather
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
	remote, err := ca.RemoteNode(ctx)
	if err != nil {
		return nil, err
	}

	if request.SubscriptionID == "" {
		return nil, grpc.Errorf(codes.InvalidArgument, "missing subscription ID")
	}

	// Make sure logs are emitted using the right Node ID to avoid impersonation.
	for _, msg := range request.Messages {
		if msg.Context.NodeID != remote.NodeID {
			return nil, grpc.Errorf(codes.PermissionDenied, "invalid NodeID: expected=%s;received=%s", remote.NodeID, msg.Context.NodeID)
		}
	}

	lb.logQueue.Publish(request)
	return &api.PublishLogsResponse{}, nil
}
