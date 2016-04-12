package state

import (
	"github.com/docker/go-events"
	"github.com/docker/swarm-v2/manager/state/watch"
	objectspb "github.com/docker/swarm-v2/pb/docker/cluster/objects"
)

// Event is the type used for events passed over watcher channels, and also
// the type used to specify filtering in calls to Watch.
type Event interface {
	// TODO(stevvooe): Consider whether it makes sense to squish both the
	// matcher type and the primary type into the same type. It might be better
	// to build a matcher from an event prototype.

	// matches checks if this item in a watch queue matches the event
	// description.
	matches(events.Event) bool
}

// EventCommit delineates a transaction boundary.
type EventCommit struct{}

func (e EventCommit) matches(watchEvent events.Event) bool {
	_, ok := watchEvent.(EventCommit)
	return ok
}

// TaskCheckFunc is the type of function used to perform filtering checks on
// api.Task structures.
type TaskCheckFunc func(t1, t2 *objectspb.Task) bool

// TaskCheckID is a TaskCheckFunc for matching task IDs.
func TaskCheckID(t1, t2 *objectspb.Task) bool {
	return t1.ID == t2.ID
}

// TaskCheckNodeID is a TaskCheckFunc for matching node IDs.
func TaskCheckNodeID(t1, t2 *objectspb.Task) bool {
	return t1.NodeID == t2.NodeID
}

// EventCreateTask is the type used to put CreateTask events on the
// publish/subscribe queue and filter these events in calls to Watch.
type EventCreateTask struct {
	Task *objectspb.Task
	// Checks is a list of functions to call to filter events for a watch
	// stream. They are applied with AND logic. They are only applicable for
	// calls to Watch.
	Checks []TaskCheckFunc
}

func (e EventCreateTask) matches(watchEvent events.Event) bool {
	typedEvent, ok := watchEvent.(EventCreateTask)
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
	Task *objectspb.Task
	// Checks is a list of functions to call to filter events for a watch
	// stream. They are applied with AND logic. They are only applicable for
	// calls to Watch.
	Checks []TaskCheckFunc
}

func (e EventUpdateTask) matches(watchEvent events.Event) bool {
	typedEvent, ok := watchEvent.(EventUpdateTask)
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
	Task *objectspb.Task
	// Checks is a list of functions to call to filter events for a watch
	// stream. They are applied with AND logic. They are only applicable for
	// calls to Watch.
	Checks []TaskCheckFunc
}

func (e EventDeleteTask) matches(watchEvent events.Event) bool {
	typedEvent, ok := watchEvent.(EventDeleteTask)
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
type JobCheckFunc func(j1, j2 *objectspb.Job) bool

// JobCheckID is a JobCheckFunc for matching job IDs.
func JobCheckID(j1, j2 *objectspb.Job) bool {
	return j1.ID == j2.ID
}

// EventCreateJob is the type used to put CreateJob events on the
// publish/subscribe queue and filter these events in calls to Watch.
type EventCreateJob struct {
	Job *objectspb.Job
	// Checks is a list of functions to call to filter events for a watch
	// stream. They are applied with AND logic. They are only applicable for
	// calls to Watch.
	Checks []JobCheckFunc
}

func (e EventCreateJob) matches(watchEvent events.Event) bool {
	typedEvent, ok := watchEvent.(EventCreateJob)
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
	Job *objectspb.Job
	// Checks is a list of functions to call to filter events for a watch
	// stream. They are applied with AND logic. They are only applicable for
	// calls to Watch.
	Checks []JobCheckFunc
}

func (e EventUpdateJob) matches(watchEvent events.Event) bool {
	typedEvent, ok := watchEvent.(EventUpdateJob)
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
	Job *objectspb.Job
	// Checks is a list of functions to call to filter events for a watch
	// stream. They are applied with AND logic. They are only applicable for
	// calls to Watch.
	Checks []JobCheckFunc
}

func (e EventDeleteJob) matches(watchEvent events.Event) bool {
	typedEvent, ok := watchEvent.(EventDeleteJob)
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
type NetworkCheckFunc func(n1, n2 *objectspb.Network) bool

// NetworkCheckID is a NetworkCheckFunc for matching network IDs.
func NetworkCheckID(n1, n2 *objectspb.Network) bool {
	return n1.ID == n2.ID
}

// EventCreateNetwork is the type used to put CreateNetwork events on the
// publish/subscribe queue and filter these events in calls to Watch.
type EventCreateNetwork struct {
	Network *objectspb.Network
	// Checks is a list of functions to call to filter events for a watch
	// stream. They are applied with AND logic. They are only applicable for
	// calls to Watch.
	Checks []NetworkCheckFunc
}

func (e EventCreateNetwork) matches(watchEvent events.Event) bool {
	typedEvent, ok := watchEvent.(EventCreateNetwork)
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
	Network *objectspb.Network
	// Checks is a list of functions to call to filter events for a watch
	// stream. They are applied with AND logic. They are only applicable for
	// calls to Watch.
	Checks []NetworkCheckFunc
}

func (e EventUpdateNetwork) matches(watchEvent events.Event) bool {
	typedEvent, ok := watchEvent.(EventUpdateNetwork)
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
	Network *objectspb.Network
	// Checks is a list of functions to call to filter events for a watch
	// stream. They are applied with AND logic. They are only applicable for
	// calls to Watch.
	Checks []NetworkCheckFunc
}

func (e EventDeleteNetwork) matches(watchEvent events.Event) bool {
	typedEvent, ok := watchEvent.(EventDeleteNetwork)
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
type NodeCheckFunc func(n1, n2 *objectspb.Node) bool

// NodeCheckID is a NodeCheckFunc for matching node IDs.
func NodeCheckID(n1, n2 *objectspb.Node) bool {
	return n1.ID == n2.ID
}

// NodeCheckStatus is a NodeCheckFunc for matching node status.
func NodeCheckStatus(n1, n2 *objectspb.Node) bool {
	return n1.Status == n2.Status
}

// EventCreateNode is the type used to put CreateNode events on the
// publish/subscribe queue and filter these events in calls to Watch.
type EventCreateNode struct {
	Node *objectspb.Node
	// Checks is a list of functions to call to filter events for a watch
	// stream. They are applied with AND logic. They are only applicable for
	// calls to Watch.
	Checks []NodeCheckFunc
}

func (e EventCreateNode) matches(watchEvent events.Event) bool {
	typedEvent, ok := watchEvent.(EventCreateNode)
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
	Node *objectspb.Node
	// Checks is a list of functions to call to filter events for a watch
	// stream. They are applied with AND logic. They are only applicable for
	// calls to Watch.
	Checks []NodeCheckFunc
}

func (e EventUpdateNode) matches(watchEvent events.Event) bool {
	typedEvent, ok := watchEvent.(EventUpdateNode)
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
	Node *objectspb.Node
	// Checks is a list of functions to call to filter events for a watch
	// stream. They are applied with AND logic. They are only applicable for
	// calls to Watch.
	Checks []NodeCheckFunc
}

func (e EventDeleteNode) matches(watchEvent events.Event) bool {
	typedEvent, ok := watchEvent.(EventDeleteNode)
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

// VolumeCheckFunc is the type of function used to perform filtering checks on
// api.Volume structures.
type VolumeCheckFunc func(v1, v2 *objectspb.Volume) bool

// VolumeCheckID is a VolumeCheckFunc for matching volume IDs.
func VolumeCheckID(v1, v2 *objectspb.Volume) bool {
	return v1.ID == v2.ID
}

// EventCreateVolume is the type used to put CreateVolume events on the
// publish/subscribe queue and filter these events in calls to Watch.
type EventCreateVolume struct {
	Volume *objectspb.Volume
	// Checks is a list of functions to call to filter events for a watch
	// stream. They are applied with AND logic. They are only applicable for
	// calls to Watch.
	Checks []VolumeCheckFunc
}

func (e EventCreateVolume) matches(watchEvent events.Event) bool {
	typedEvent, ok := watchEvent.(EventCreateVolume)
	if !ok {
		return false
	}

	for _, check := range e.Checks {
		if !check(e.Volume, typedEvent.Volume) {
			return false
		}
	}
	return true
}

// EventUpdateVolume is the type used to put UpdateVolume events on the
// publish/subscribe queue and filter these events in calls to Watch.
type EventUpdateVolume struct {
	Volume *objectspb.Volume
	// Checks is a list of functions to call to filter events for a watch
	// stream. They are applied with AND logic. They are only applicable for
	// calls to Watch.
	Checks []VolumeCheckFunc
}

func (e EventUpdateVolume) matches(watchEvent events.Event) bool {
	typedEvent, ok := watchEvent.(EventUpdateVolume)
	if !ok {
		return false
	}

	for _, check := range e.Checks {
		if !check(e.Volume, typedEvent.Volume) {
			return false
		}
	}
	return true
}

// EventDeleteVolume is the type used to put DeleteVolume events on the
// publish/subscribe queue and filter these events in calls to Watch.
type EventDeleteVolume struct {
	Volume *objectspb.Volume
	// Checks is a list of functions to call to filter events for a watch
	// stream. They are applied with AND logic. They are only applicable for
	// calls to Watch.
	Checks []VolumeCheckFunc
}

func (e EventDeleteVolume) matches(watchEvent events.Event) bool {
	typedEvent, ok := watchEvent.(EventDeleteVolume)
	if !ok {
		return false
	}

	for _, check := range e.Checks {
		if !check(e.Volume, typedEvent.Volume) {
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
func Watch(queue *watch.Queue, specifiers ...Event) (eventq chan events.Event, cancel func()) {
	if len(specifiers) == 0 {
		return queue.Watch()
	}
	return queue.CallbackWatch(events.MatcherFunc(func(event events.Event) bool {
		for _, s := range specifiers {
			if s.matches(event) {
				return true
			}
		}
		return false
	}))
}
