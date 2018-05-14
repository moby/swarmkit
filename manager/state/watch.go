package state

import (
	"github.com/docker/go-events"
	"github.com/docker/swarmkit/api"
)

// EventCommit delineates a transaction boundary.
type EventCommit struct {
	Version *api.Version
}

// Matches returns true if this event is a commit event.
func (e EventCommit) Matches(watchEvent events.Event) bool {
	_, ok := watchEvent.(EventCommit)
	return ok
}

// TaskCheckStateGreaterThan is a TaskCheckFunc for checking task state.
func TaskCheckStateGreaterThan(t1, t2 *api.Task) bool {
	return t2.Status.State > t1.Status.State
}

// NodeCheckState is a NodeCheckFunc for matching node state.
func NodeCheckState(n1, n2 *api.Node) bool {
	return n1.Status.State == n2.Status.State
}

// Matcher returns an events.Matcher that Matches the specifiers with OR logic.
// The subscriber will receive events that match any of the arguments passed to Matcher.
//
// Examples:
//
// // subscribe to all events
// q.Watch()
// // or
// q.CallbackWatch(Matcher())
//
// // subscribe to all UpdateTask events
// q.CallbackWatch(Matcher(EventUpdateTask{}))
//
// // subscribe to all task-related events
// q.CallbackWatch(Matcher(
// 	api.EventUpdateTask{},
// 	api.EventCreateTask{},
// 	api.EventDeleteTask{},
// ))
//
// // subscribe to UpdateTask for node 123
// q.CallbackWatch(Matcher(
// 	api.EventUpdateTask{
// 		Task: &api.Task{NodeID: 123},
// 		Checks: []TaskCheckFunc{TaskCheckNodeID},
// 	},
// ))
//
// // subscribe to UpdateTask for node 123, as well as CreateTask
// // for node 123 that also has ServiceID set to "abc"
// q.CallbackWatch(Matcher(
// 	api.EventUpdateTask{
// 		Task: &api.Task{NodeID: 123},
// 		Checks: []TaskCheckFunc{TaskCheckNodeID},
// 	},
// 	api.EventCreateTask{
// 		Task: &api.Task{NodeID: 123, ServiceID: "abc"},
// 		Checks: []TaskCheckFunc{
// 			TaskCheckNodeID,
// 			func(t1, t2 *api.Task) bool {return t1.ServiceID == t2.ServiceID},
// 		},
// 	},
// ))
func Matcher(specifiers ...api.Event) events.MatcherFunc {
	if len(specifiers) == 0 {
		return nil
	}
	return events.MatcherFunc(func(event events.Event) bool {
		for _, s := range specifiers {
			if s.Matches(event) {
				return true
			}
		}
		return false
	})
}
