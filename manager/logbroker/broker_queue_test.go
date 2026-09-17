package logbroker

import (
	"context"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/moby/swarmkit/v2/api"
	"github.com/moby/swarmkit/v2/manager/state/store"
	"github.com/stretchr/testify/require"
)

// subscriptionRetention drives `count` subscriptions through their full,
// correct lifecycle (Run -> register -> unregister -> Stop) against a broker
// whose only ListenSubscriptions watcher behaves as described by `drain`, and
// reports how many of those subscriptions the runtime was able to reclaim
// afterwards.
//
// A subscription that has been unregistered is no longer referenced by any of
// the broker's bookkeeping maps, so a correctly behaving broker must allow it
// to be collected regardless of what the watcher is doing.
func subscriptionRetention(t *testing.T, count int, drain bool) (reclaimed int64) {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	s := store.NewMemoryStore(nil)
	require.NotNil(t, s)
	defer s.Close()

	broker := New(s)
	require.NoError(t, broker.Start(ctx))
	defer broker.Stop()

	const nodeID = "node-stalled"
	broker.nodeConnected(nodeID)

	// Stand in for ListenSubscriptions: it registers a watch on the
	// subscriptionQueue and, in the failure case, stops reading from the
	// channel. That is what happens in ListenSubscriptions when stream.Send
	// blocks on a worker whose gRPC stream has stalled -- the loop never gets
	// back around to `case v := <-subscriptionCh`.
	_, subscriptionCh, cancelWatch := broker.watchSubscriptions(nodeID)
	defer cancelWatch()

	stopDraining := make(chan struct{})
	defer close(stopDraining)
	if drain {
		go func() {
			for {
				select {
				case <-subscriptionCh:
				case <-stopDraining:
					return
				}
			}
		}()
	}

	var collected atomic.Int64
	for range count {
		sub := broker.newSubscription(
			&api.LogSelector{NodeIDs: []string{nodeID}},
			&api.LogSubscriptionOptions{},
		)
		runtime.SetFinalizer(sub, func(*subscription) { collected.Add(1) })

		// Mirror SubscribeLogs exactly, including every cleanup step it
		// performs on return.
		sub.Run(ctx)
		broker.registerSubscription(sub)
		broker.unregisterSubscription(sub)
		sub.Stop()
	}

	// Nothing in this function still references the subscriptions. Give the
	// collector several opportunities to reclaim them and to run finalizers.
	for range 5 {
		runtime.GC()
		time.Sleep(50 * time.Millisecond)
	}
	runtime.GC()
	time.Sleep(100 * time.Millisecond)

	return collected.Load()
}

// TestLogBrokerSubscriptionQueueBounded asserts that a ListenSubscriptions
// watcher which stops draining its channel cannot pin an unbounded number of
// subscriptions.
//
// A watcher stops draining whenever stream.Send blocks, which happens when the
// agent's gRPC stream stalls. registerSubscription and unregisterSubscription
// both publish the *subscription to subscriptionQueue, so an unbounded queue
// lets a single stalled watcher retain every subscription that has passed
// through the broker -- along with its SubscriptionMessage, LogSelector,
// LogSubscriptionOptions and cancel context -- even though each subscription
// was unregistered correctly.
//
// Because the backlog accumulates in a container/list rather than in blocked
// goroutines, such a leak is invisible to goroutine-count based monitoring and
// shows up only as unexplained heap growth in the manager.
func TestLogBrokerSubscriptionQueueBounded(t *testing.T) {
	// Push well past the limit so a bounded queue has to shed load.
	const count = 3 * subscriptionQueueLimit

	t.Run("draining watcher", func(t *testing.T) {
		reclaimed := subscriptionRetention(t, count, true)
		t.Logf("reclaimed %d/%d subscriptions", reclaimed, count)
		require.EqualValues(t, count, reclaimed,
			"a watcher that drains its channel must not pin unregistered subscriptions")
	})

	t.Run("stalled watcher", func(t *testing.T) {
		reclaimed := subscriptionRetention(t, count, false)
		t.Logf("reclaimed %d/%d subscriptions", reclaimed, count)
		require.GreaterOrEqual(t, reclaimed, int64(count-subscriptionQueueLimit),
			"a stalled watcher must not retain more than subscriptionQueueLimit subscriptions")
	})
}
