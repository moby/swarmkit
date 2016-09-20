package logbroker

import (
	"sync"

	"github.com/docker/go-events"
	"github.com/docker/swarmkit/api"
	"github.com/docker/swarmkit/manager/state/store"
	"golang.org/x/net/context"
)

// LogBroker coordinates log subscriptions to services and tasks. Çlients can
// publish and subscribe to logs channels.
//
// Log subscriptions are pushed to the work nodes by creating log subscsription
// tasks. As such, the LogBroker also acts as an orchestrator of these tasks.
type LogBroker struct {
	store       *store.MemoryStore
	broadcaster *events.Broadcaster
	nodes       []*api.Node
	mu          sync.RWMutex
}

func NewLogBroker() *LogBroker {
	return &LogBroker{
		broadcaster: events.NewBroadcaster(),
	}
}

func (lb *LogBroker) SubscribeLogs(request *api.SubscribeLogsRequest, stream api.Logs_SubscribeLogsServer) error {
	ch := events.NewChannel(1000)
	q := events.NewQueue(ch)
	lb.broadcaster.Add(q)

	defer func() {
		lb.broadcaster.Remove(q)
		q.Close()
		ch.Close()
	}()

	ctx := stream.Context()

	for {
		select {
		case v := <-ch.C:
			msgs, ok := v.([]api.LogMessage)
			if !ok {
				continue
			}

			if err := stream.Send(&api.SubscribeLogsMessage{
				Messages: msgs,
			}); err != nil {
				return err
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	return nil
}

func (lb *LogBroker) ListenSubscriptions(request *api.ListenSubscriptionsRequest, stream api.LogBroker_ListenSubscriptionsServer) error {
	return nil
}

func (lb *LogBroker) PublishLogs(ctx context.Context, request *api.PublishLogsRequest) (*api.PublishLogsResponse, error) {
	// log.G(ctx).Infof("received %v", request.Messages)
	lb.broadcaster.Write(request.Messages)
	return &api.PublishLogsResponse{}, nil
}
