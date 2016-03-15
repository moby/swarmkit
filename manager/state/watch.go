package state

import (
	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/manager/state/watch"
)

// Event is the type used for events passed over watcher channels, and also
// the type used to specify filtering in calls to Watch.
type Event interface {
	// matches checks if this item in a watch queue matches the event
	// description.
	matches(watch.Event) bool
}

// EventCommit delineates a transaction boundary.
type EventCommit struct{}

func (e EventCommit) matches(watchEvent watch.Event) bool {
	_, ok := watchEvent.Payload.(EventCommit)
	return ok
}

// TaskCheckFunc is the type of function used to perform filtering checks on
// api.Task structures.
type TaskCheckFunc func(t1, t2 *api.Task) bool

// TaskCheckID is a TaskCheckFunc for matching task IDs.
func TaskCheckID(t1, t2 *api.Task) bool {
	return t1.ID == t2.ID
}

// TaskCheckNodeID is a TaskCheckFunc for matching node IDs.
func TaskCheckNodeID(t1, t2 *api.Task) bool {
	return t1.NodeID == t2.NodeID
}

// EventCreateTask is the type used to put CreateTask events on the
// publish/subscribe queue and filter these events in calls to Watch.
type EventCreateTask struct {
	Task *api.Task
	// Checks is a list of functions to call to filter events for a watch
	// stream. They are applied with AND logic. They are only applicable for
	// calls to Watch.
	Checks []TaskCheckFunc
}

func (e EventCreateTask) matches(watchEvent watch.Event) bool {
	typedEvent, ok := watchEvent.Payload.(EventCreateTask)
	if !ok {
		return false
	}

	for _, check := range e.Checks {
		if !check(e.Task, typedEvent.Task) {
			return false
		}
	}
	return true
}

// EventUpdateTask is the type used to put UpdateTask events on the
// publish/subscribe queue and filter these events in calls to Watch.
type EventUpdateTask struct {
	Task *api.Task
	// Checks is a list of functions to call to filter events for a watch
	// stream. They are applied with AND logic. They are only applicable for
	// calls to Watch.
	Checks []TaskCheckFunc
}

func (e EventUpdateTask) matches(watchEvent watch.Event) bool {
	typedEvent, ok := watchEvent.Payload.(EventUpdateTask)
	if !ok {
		return false
	}

	for _, check := range e.Checks {
		if !check(e.Task, typedEvent.Task) {
			return false
		}
	}
	return true
}

// EventDeleteTask is the type used to put DeleteTask events on the
// publish/subscribe queue and filter these events in calls to Watch.
type EventDeleteTask struct {
	Task *api.Task
	// Checks is a list of functions to call to filter events for a watch
	// stream. They are applied with AND logic. They are only applicable for
	// calls to Watch.
	Checks []TaskCheckFunc
}

func (e EventDeleteTask) matches(watchEvent watch.Event) bool {
	typedEvent, ok := watchEvent.Payload.(EventDeleteTask)
	if !ok {
		return false
	}

	for _, check := range e.Checks {
		if !check(e.Task, typedEvent.Task) {
			return false
		}
	}
	return true
}

// JobCheckFunc is the type of function used to perform filtering checks on
// api.Job structures.
type JobCheckFunc func(j1, j2 *api.Job) bool

// JobCheckID is a JobCheckFunc for matching job IDs.
func JobCheckID(j1, j2 *api.Job) bool {
	return j1.ID == j2.ID
}

// EventCreateJob is the type used to put CreateJob events on the
// publish/subscribe queue and filter these events in calls to Watch.
type EventCreateJob struct {
	Job *api.Job
	// Checks is a list of functions to call to filter events for a watch
	// stream. They are applied with AND logic. They are only applicable for
	// calls to Watch.
	Checks []JobCheckFunc
}

func (e EventCreateJob) matches(watchEvent watch.Event) bool {
	typedEvent, ok := watchEvent.Payload.(EventCreateJob)
	if !ok {
		return false
	}

	for _, check := range e.Checks {
		if !check(e.Job, typedEvent.Job) {
			return false
		}
	}
	return true
}

// EventUpdateJob is the type used to put UpdateJob events on the
// publish/subscribe queue and filter these events in calls to Watch.
type EventUpdateJob struct {
	Job *api.Job
	// Checks is a list of functions to call to filter events for a watch
	// stream. They are applied with AND logic. They are only applicable for
	// calls to Watch.
	Checks []JobCheckFunc
}

func (e EventUpdateJob) matches(watchEvent watch.Event) bool {
	typedEvent, ok := watchEvent.Payload.(EventUpdateJob)
	if !ok {
		return false
	}

	for _, check := range e.Checks {
		if !check(e.Job, typedEvent.Job) {
			return false
		}
	}
	return true
}

// EventDeleteJob is the type used to put DeleteJob events on the
// publish/subscribe queue and filter these events in calls to Watch.
type EventDeleteJob struct {
	Job *api.Job
	// Checks is a list of functions to call to filter events for a watch
	// stream. They are applied with AND logic. They are only applicable for
	// calls to Watch.
	Checks []JobCheckFunc
}

func (e EventDeleteJob) matches(watchEvent watch.Event) bool {
	typedEvent, ok := watchEvent.Payload.(EventDeleteJob)
	if !ok {
		return false
	}

	for _, check := range e.Checks {
		if !check(e.Job, typedEvent.Job) {
			return false
		}
	}
	return true
}

// NetworkCheckFunc is the type of function used to perform filtering checks on
// api.Job structures.
type NetworkCheckFunc func(n1, n2 *api.Network) bool

// NetworkCheckID is a NetworkCheckFunc for matching network IDs.
func NetworkCheckID(n1, n2 *api.Network) bool {
	return n1.ID == n2.ID
}

// EventCreateNetwork is the type used to put CreateNetwork events on the
// publish/subscribe queue and filter these events in calls to Watch.
type EventCreateNetwork struct {
	Network *api.Network
	// Checks is a list of functions to call to filter events for a watch
	// stream. They are applied with AND logic. They are only applicable for
	// calls to Watch.
	Checks []NetworkCheckFunc
}

func (e EventCreateNetwork) matches(watchEvent watch.Event) bool {
	typedEvent, ok := watchEvent.Payload.(EventCreateNetwork)
	if !ok {
		return false
	}

	for _, check := range e.Checks {
		if !check(e.Network, typedEvent.Network) {
			return false
		}
	}
	return true
}

// EventUpdateNetwork is the type used to put UpdateNetwork events on the
// publish/subscribe queue and filter these events in calls to Watch.
type EventUpdateNetwork struct {
	Network *api.Network
	// Checks is a list of functions to call to filter events for a watch
	// stream. They are applied with AND logic. They are only applicable for
	// calls to Watch.
	Checks []NetworkCheckFunc
}

func (e EventUpdateNetwork) matches(watchEvent watch.Event) bool {
	typedEvent, ok := watchEvent.Payload.(EventUpdateNetwork)
	if !ok {
		return false
	}

	for _, check := range e.Checks {
		if !check(e.Network, typedEvent.Network) {
			return false
		}
	}
	return true
}

// EventDeleteNetwork is the type used to put DeleteNetwork events on the
// publish/subscribe queue and filter these events in calls to Watch.
type EventDeleteNetwork struct {
	Network *api.Network
	// Checks is a list of functions to call to filter events for a watch
	// stream. They are applied with AND logic. They are only applicable for
	// calls to Watch.
	Checks []NetworkCheckFunc
}

func (e EventDeleteNetwork) matches(watchEvent watch.Event) bool {
	typedEvent, ok := watchEvent.Payload.(EventDeleteNetwork)
	if !ok {
		return false
	}

	for _, check := range e.Checks {
		if !check(e.Network, typedEvent.Network) {
			return false
		}
	}
	return true
}

// NodeCheckFunc is the type of function used to perform filtering checks on
// api.Job structures.
type NodeCheckFunc func(n1, n2 *api.Node) bool

// NodeCheckID is a NodeCheckFunc for matching node IDs.
func NodeCheckID(n1, n2 *api.Node) bool {
	return n1.ID == n2.ID
}

// NodeCheckStatus is a NodeCheckFunc for matching node status.
func NodeCheckStatus(n1, n2 *api.Node) bool {
	return n1.Status == n2.Status
}

// EventCreateNode is the type used to put CreateNode events on the
// publish/subscribe queue and filter these events in calls to Watch.
type EventCreateNode struct {
	Node *api.Node
	// Checks is a list of functions to call to filter events for a watch
	// stream. They are applied with AND logic. They are only applicable for
	// calls to Watch.
	Checks []NodeCheckFunc
}

func (e EventCreateNode) matches(watchEvent watch.Event) bool {
	typedEvent, ok := watchEvent.Payload.(EventCreateNode)
	if !ok {
		return false
	}

	for _, check := range e.Checks {
		if !check(e.Node, typedEvent.Node) {
			return false
		}
	}
	return true
}

// EventUpdateNode is the type used to put DeleteNode events on the
// publish/subscribe queue and filter these events in calls to Watch.
type EventUpdateNode struct {
	Node *api.Node
	// Checks is a list of functions to call to filter events for a watch
	// stream. They are applied with AND logic. They are only applicable for
	// calls to Watch.
	Checks []NodeCheckFunc
}

func (e EventUpdateNode) matches(watchEvent watch.Event) bool {
	typedEvent, ok := watchEvent.Payload.(EventUpdateNode)
	if !ok {
		return false
	}

	for _, check := range e.Checks {
		if !check(e.Node, typedEvent.Node) {
			return false
		}
	}
	return true
}

// EventDeleteNode is the type used to put DeleteNode events on the
// publish/subscribe queue and filter these events in calls to Watch.
type EventDeleteNode struct {
	Node *api.Node
	// Checks is a list of functions to call to filter events for a watch
	// stream. They are applied with AND logic. They are only applicable for
	// calls to Watch.
	Checks []NodeCheckFunc
}

func (e EventDeleteNode) matches(watchEvent watch.Event) bool {
	typedEvent, ok := watchEvent.Payload.(EventDeleteNode)
	if !ok {
		return false
	}

	for _, check := range e.Checks {
		if !check(e.Node, typedEvent.Node) {
			return false
		}
	}
	return true
}

// Watch takes a variable number of events to match against. The subscriber
// will receive events that match any of the arguments passed to Watch.
//
// Examples:
//
// // subscribe to all events
// Watch(q)
//
// // subscribe to all UpdateTask events
// Watch(q, EventUpdateTask{})
//
// // subscribe to all task-related events
// Watch(q, EventUpdateTask{}, EventCreateTask{}, EventDeleteTask{})
//
// // subscribe to UpdateTask for node 123
// Watch(q, EventUpdateTask{Task: &api.Task{NodeID: 123},
//                         Checks: []TaskCheckFunc{TaskCheckNodeID}})
//
// // subscribe to UpdateTask for node 123, as well as CreateTask
// // for node 123 that also has JobID set to "abc"
// Watch(q, EventUpdateTask{Task: &api.Task{NodeID: 123},
//                         Checks: []TaskCheckFunc{TaskCheckNodeID}},
//         EventCreateTask{Task: &api.Task{NodeID: 123, JobID: "abc"},
//                         Checks: []TaskCheckFunc{TaskCheckNodeID,
//                                                 func(t1, t2 *api.Task) bool {
//                                                         return t1.JobID == t2.JobID
//                                                 }}})
func Watch(queue *watch.Queue, specifiers ...Event) chan watch.Event {
	if len(specifiers) == 0 {
		return queue.Watch()
	}
	return queue.CallbackWatch(func(event watch.Event) bool {
		for _, s := range specifiers {
			if s.matches(event) {
				return true
			}
		}
		return false
	})
}

// Publish publishes an event to a queue.
func Publish(queue *watch.Queue, event Event) {
	queue.Publish(watch.Event{Payload: event})
}
