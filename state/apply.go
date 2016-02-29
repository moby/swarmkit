package state

import (
	"errors"

	"github.com/docker/swarm-v2/watch"
)

// Apply takes an item from the event stream of one Store and applies it to
// a second Store.
func Apply(store Store, item watch.Event) error {
	switch v := item.Payload.(type) {
	case EventCreateTask:
		return store.CreateTask(v.Task.ID, v.Task)
	case EventUpdateTask:
		return store.UpdateTask(v.Task.ID, v.Task)
	case EventDeleteTask:
		return store.DeleteTask(v.Task.ID)

	case EventCreateJob:
		return store.CreateJob(v.Job.ID, v.Job)
	case EventUpdateJob:
		return store.UpdateJob(v.Job.ID, v.Job)
	case EventDeleteJob:
		return store.DeleteJob(v.Job.ID)

	case EventCreateNode:
		return store.CreateNode(v.Node.Spec.ID, v.Node)
	case EventUpdateNode:
		return store.UpdateNode(v.Node.Spec.ID, v.Node)
	case EventDeleteNode:
		return store.DeleteNode(v.Node.Spec.ID)

	}

	return errors.New("unrecognized event type")
}
