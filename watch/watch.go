package watch

// Queue is the structure used to publish events and watch for them.
type Queue struct {
	pub *publisher
}

// Event is a struct wrapping objects sent through the queue.
type Event struct {
	// Payload is the actual object being passed through the queue.
	Payload interface{}
}

// NewQueue creates a new publish/subscribe queue which supports watchers.
// The channels that it will create for subscriptions will have the buffer
// size specified by buffer.
func NewQueue(buffer int) *Queue {
	return &Queue{
		pub: newPublisher(buffer),
	}
}

// Watch returns a channel which will receive all items published to the
// queue from this point, until StopWatch is closed.
func (q *Queue) Watch() chan Event {
	return q.pub.subscribe()
}

// CallbackWatch returns a channel which will receive all events published to
// the queue from this point that pass the check in the provided callback
// function. StopWatch will stop the flow of events and close the channel.
func (q *Queue) CallbackWatch(topicFunc topicFunc) chan Event {
	return q.pub.subscribeTopic(topicFunc)
}

// StopWatch stops a watcher from receiving further events, and closes its
// channel.
func (q *Queue) StopWatch(ch chan Event) {
	q.pub.evict(ch)
}

// Publish adds an item to the queue.
func (q *Queue) Publish(item Event) {
	q.pub.publish(item)
}
