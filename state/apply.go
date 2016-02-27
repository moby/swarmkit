package state

import (
	"errors"

	"github.com/docker/swarm-v2/watch"
)

// Apply takes an item from the event stream of one Store and applies it to
// a second Store.
func Apply(store Store, item watch.Event) (err error) {
	tx, err := store.Begin()
	if err != nil {
		return err
	}
	defer func() {
		closeErr := tx.Close()
		if closeErr != nil && err == nil {
			err = closeErr
		}
	}()

	switch v := item.Payload.(type) {
	case EventCreateTask:
		return tx.Tasks().Create(v.Task)
	case EventUpdateTask:
		return tx.Tasks().Update(v.Task)
	case EventDeleteTask:
		return tx.Tasks().Delete(v.Task.ID)

	case EventCreateJob:
		return tx.Jobs().Create(v.Job)
	case EventUpdateJob:
		return tx.Jobs().Update(v.Job)
	case EventDeleteJob:
		return tx.Jobs().Delete(v.Job.ID)

	case EventCreateNode:
		return tx.Nodes().Create(v.Node)
	case EventUpdateNode:
		return tx.Nodes().Update(v.Node)
	case EventDeleteNode:
		return tx.Nodes().Delete(v.Node.Spec.ID)

	}

	return errors.New("unrecognized event type")
}
