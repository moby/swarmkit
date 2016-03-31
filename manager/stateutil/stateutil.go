package stateutil

import (
	"errors"

	"github.com/Sirupsen/logrus"
	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/manager/state"
)

// NodeReady marks node as ready in store. It creates new node or updates existing.
func NodeReady(store state.Store, id string, desc *api.NodeDescription) (*api.Node, error) {
	// create or update node in store
	// TODO(stevvooe): Validate node specification.
	var node *api.Node
	err := store.Update(func(tx state.Tx) error {
		node = tx.Nodes().Get(id)
		if node != nil {
			node.Description = desc
			node.Status = api.NodeStatus{
				State: api.NodeStatus_READY,
			}
			return tx.Nodes().Update(node)
		}

		node = &api.Node{
			ID:          id,
			Description: desc,
			Status: api.NodeStatus{
				State: api.NodeStatus_READY,
			},
		}
		return tx.Nodes().Create(node)
	})
	return node, err
}

// UpdateTasks updates statuses of multiple tasks.
func UpdateTasks(store state.Store, us []*api.TaskStatusUpdate) error {
	return store.Update(func(tx state.Tx) error {
		for _, u := range us {
			logger := logrus.WithField("task.id", u.TaskID)
			if u.Status == nil {
				logger.Warnf("task report has nil status")
				continue
			}
			task := tx.Tasks().Get(u.TaskID)
			if task == nil {
				logger.Errorf("task unavailable")
				continue
			}

			var state api.TaskState
			if task.Status != nil {
				state = task.Status.State
			} else {
				state = api.TaskStateNew
			}

			logger.Debugf("%v -> %v", state, u.Status.State)
			task.Status = u.Status

			if err := tx.Tasks().Update(task); err != nil {
				return err
			}
		}
		return nil
	})
}

// UpdateNodeStatus updates status of node with particular id.
func UpdateNodeStatus(store state.Store, id string, status api.NodeStatus) error {
	return store.Update(func(tx state.Tx) error {
		node := tx.Nodes().Get(id)
		if node == nil {
			return errors.New("node not found")
		}
		node.Status = status
		return tx.Nodes().Update(node)
	})
}
