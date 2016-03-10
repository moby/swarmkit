package watch

import (
	"testing"
	"time"
)

func TestPubSub(t *testing.T) {
	pub := newPublisher(0)

	// Create 5 subscribers. Each one only subscribes to events that are
	// multiples of the subscriber number.
	var chans [5]chan Event
	chans[0] = pub.subscribe()
	for i := 1; i < 5; i++ {
		chans[i] = pub.subscribeTopic(createTopicFunc(i + 1))
	}

	if pub.length() != 5 {
		t.Fatal("wrong number of subscribers")
	}

	// Send 1000 events. This should not hang even though nothing is
	// reading from the subscriber channels yet.
	for i := 0; i < 1000; i++ {
		pub.publish(Event{Payload: i})
	}

	// Make sure the subscribers get all the appropriate
	// events, in the correct order.
	for i := 0; i < 1000; i++ {
		for j := 0; j < 5; j++ {
			if i%(j+1) == 0 {
				event := <-chans[j]
				if i != event.Payload.(int) {
					t.Fatalf("event mismatch: chan %d received %d, expected %d", j, event.Payload.(int), i)
				}
			}
		}
	}

	// Evict one of the subscribers. Make sure its channel gets closed.
	pub.evict(chans[2])
	select {
	case <-chans[2]:
	case <-time.After(time.Second):
		t.Fatal("channel was not closed on evict")
	}

	// Close the publisher.
	pub.closePublisher()

	// Make sure that all subscriber channels get closed.
	for i := 0; i < 5; i++ {
		select {
		case <-chans[i]:
		case <-time.After(time.Second):
			t.Fatalf("channel %d was not closed", i)
		}
	}

	// Make sure that a send at this point doesn't blow up.
	pub.publish(Event{Payload: 1234})
}

func createTopicFunc(i int) topicFunc {
	return func(x Event) bool {
		xi := x.Payload.(int)
		return (xi%i == 0)
	}
}
