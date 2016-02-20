package watch

import (
	"container/list"
	"sync"
)

// publisher is basic pub/sub structure. It allows sending events and
// subscribing to them. It can be safely used from multiple goroutines.
// This implementation guarantees that no events are lost. It also
// guarantees that events submitted in a certain order will be delivered
// in that order.
type publisher struct {
	mu          sync.Mutex
	buffer      int
	subscribers map[chan<- interface{}]*subscriber
	cond        *sync.Cond
}

type subscriber struct {
	// The publisher's mutex must be locked when accessing any of these
	// subscriber queues.
	queuedEvents list.List
	topicFunc    topicFunc
	closed       chan struct{}
}

type topicFunc func(v interface{}) bool

// newPublisher creates a new pub/sub publisher to broadcast messages.
func newPublisher(buffer int) *publisher {
	pub := &publisher{
		buffer:      buffer,
		subscribers: make(map[chan<- interface{}]*subscriber),
	}
	pub.cond = sync.NewCond(&pub.mu)

	return pub
}

// length returns the number of subscribers for the publisher
func (p *publisher) length() int {
	p.mu.Lock()
	i := len(p.subscribers)
	p.mu.Unlock()
	return i
}

// subscribe adds a new subscriber to the publisher returning the channel.
func (p *publisher) subscribe() chan interface{} {
	return p.subscribeTopic(nil)
}

// subscribeTopic adds a new subscriber that filters messages sent by a topic.
func (p *publisher) subscribeTopic(topic topicFunc) chan interface{} {
	ch := make(chan interface{}, p.buffer)
	sub := &subscriber{
		topicFunc: topic,
		closed:    make(chan struct{}),
	}
	sub.queuedEvents.Init()

	p.mu.Lock()
	p.subscribers[ch] = sub
	p.mu.Unlock()

	go p.sendEvents(ch, sub)

	return ch
}

// evict removes the specified subscriber from receiving any more messages.
func (p *publisher) evict(ch chan interface{}) {
	p.mu.Lock()
	if sub, ok := p.subscribers[ch]; ok {
		delete(p.subscribers, ch)
		close(sub.closed)
	}
	p.cond.Broadcast()
	p.mu.Unlock()
}

// publish sends the data in v to all subscribers currently registered with the publisher.
func (p *publisher) publish(v interface{}) {
	p.mu.Lock()
	if len(p.subscribers) == 0 {
		p.mu.Unlock()
		return
	}

	for _, sub := range p.subscribers {
		sub.queuedEvents.PushBack(v)
	}
	p.cond.Broadcast()
	p.mu.Unlock()
}

// closePublisher closes the channels to all subscribers registered with the publisher.
func (p *publisher) closePublisher() {
	p.mu.Lock()
	for ch, sub := range p.subscribers {
		delete(p.subscribers, ch)
		close(sub.closed)
	}
	p.cond.Broadcast()
	p.mu.Unlock()
}

// sendEvents runs in a goroutine as long as the subscriber is watching for
// events. It waits for new events to be added to the queue and sends those
// over the subscriber's channel.
func (p *publisher) sendEvents(ch chan<- interface{}, sub *subscriber) {
	p.mu.Lock()
	for {
		select {
		case <-sub.closed:
			p.mu.Unlock()
			close(ch)
			return
		default:
		}

		for sub.queuedEvents.Len() > 0 {
			nextEventElem := sub.queuedEvents.Front()
			nextEvent := sub.queuedEvents.Remove(nextEventElem)

			p.mu.Unlock()

			// We do the topic check here instead of at publish
			// time so we can do it without the lock held.
			if sub.topicFunc == nil || sub.topicFunc(nextEvent) {
				select {
				case ch <- nextEvent:
				case <-sub.closed:
					return
				}
			}

			p.mu.Lock()
		}
		p.cond.Wait()
	}
}
