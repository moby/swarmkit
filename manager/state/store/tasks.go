package store

import (
	"strconv"
	"strings"

	"github.com/docker/swarmkit/api"
	"github.com/docker/swarmkit/api/naming"
	memdb "github.com/hashicorp/go-memdb"
)

const tableTask = "task"

func init() {
	register(ObjectStoreConfig{
		Name: tableTask,
		Table: &memdb.TableSchema{
			Name: tableTask,
			Indexes: map[string]*memdb.IndexSchema{
				indexID: {
					Name:    indexID,
					Unique:  true,
					Indexer: api.TaskIndexerByID{},
				},
				indexName: {
					Name:         indexName,
					AllowMissing: true,
					Indexer:      taskIndexerByName{},
				},
				indexServiceID: {
					Name:         indexServiceID,
					AllowMissing: true,
					Indexer:      taskIndexerByServiceID{},
				},
				indexNodeID: {
					Name:         indexNodeID,
					AllowMissing: true,
					Indexer:      taskIndexerByNodeID{},
				},
				indexSlot: {
					Name:         indexSlot,
					AllowMissing: true,
					Indexer:      taskIndexerBySlot{},
				},
				indexDesiredState: {
					Name:    indexDesiredState,
					Indexer: taskIndexerByDesiredState{},
				},
				indexTaskState: {
					Name:    indexTaskState,
					Indexer: taskIndexerByTaskState{},
				},
				indexNetwork: {
					Name:         indexNetwork,
					AllowMissing: true,
					Indexer:      taskIndexerByNetwork{},
				},
				indexSecret: {
					Name:         indexSecret,
					AllowMissing: true,
					Indexer:      taskIndexerBySecret{},
				},
				indexCustom: {
					Name:         indexCustom,
					Indexer:      api.TaskCustomIndexer{},
					AllowMissing: true,
				},
			},
		},
		Save: func(tx ReadTx, snapshot *api.StoreSnapshot) error {
			var err error
			snapshot.Tasks, err = FindTasks(tx, All)
			return err
		},
		Restore: func(tx Tx, snapshot *api.StoreSnapshot) error {
			tasks, err := FindTasks(tx, All)
			if err != nil {
				return err
			}
			for _, t := range tasks {
				if err := DeleteTask(tx, t.ID); err != nil {
					return err
				}
			}
			for _, t := range snapshot.Tasks {
				if err := CreateTask(tx, t); err != nil {
					return err
				}
			}
			return nil
		},
		ApplyStoreAction: func(tx Tx, sa *api.StoreAction) error {
			switch v := sa.Target.(type) {
			case *api.StoreAction_Task:
				obj := v.Task
				switch sa.Action {
				case api.StoreActionKindCreate:
					return CreateTask(tx, obj)
				case api.StoreActionKindUpdate:
					return UpdateTask(tx, obj)
				case api.StoreActionKindRemove:
					return DeleteTask(tx, obj.ID)
				}
			}
			return errUnknownStoreAction
		},
		NewStoreAction: func(c api.Event) (api.StoreAction, error) {
			var sa api.StoreAction
			switch v := c.(type) {
			case api.EventCreateTask:
				sa.Action = api.StoreActionKindCreate
				sa.Target = &api.StoreAction_Task{
					Task: v.Task,
				}
			case api.EventUpdateTask:
				sa.Action = api.StoreActionKindUpdate
				sa.Target = &api.StoreAction_Task{
					Task: v.Task,
				}
			case api.EventDeleteTask:
				sa.Action = api.StoreActionKindRemove
				sa.Target = &api.StoreAction_Task{
					Task: v.Task,
				}
			default:
				return api.StoreAction{}, errUnknownStoreAction
			}
			return sa, nil
		},
	})
}

// CreateTask adds a new task to the store.
// Returns ErrExist if the ID is already taken.
func CreateTask(tx Tx, t *api.Task) error {
	return tx.create(tableTask, t)
}

// UpdateTask updates an existing task in the store.
// Returns ErrNotExist if the node doesn't exist.
func UpdateTask(tx Tx, t *api.Task) error {
	return tx.update(tableTask, t)
}

// DeleteTask removes a task from the store.
// Returns ErrNotExist if the task doesn't exist.
func DeleteTask(tx Tx, id string) error {
	return tx.delete(tableTask, id)
}

// GetTask looks up a task by ID.
// Returns nil if the task doesn't exist.
func GetTask(tx ReadTx, id string) *api.Task {
	t := tx.get(tableTask, id)
	if t == nil {
		return nil
	}
	return t.(*api.Task)
}

// FindTasks selects a set of tasks and returns them.
func FindTasks(tx ReadTx, by By) ([]*api.Task, error) {
	checkType := func(by By) error {
		switch by.(type) {
		case byName, byNamePrefix, byIDPrefix, byDesiredState, byTaskState, byNode, byService, bySlot, byReferencedNetworkID, byReferencedSecretID, byCustom, byCustomPrefix:
			return nil
		default:
			return ErrInvalidFindBy
		}
	}

	taskList := []*api.Task{}
	appendResult := func(o api.StoreObject) {
		taskList = append(taskList, o.(*api.Task))
	}

	err := tx.find(tableTask, by, checkType, appendResult)
	return taskList, err
}

type taskIndexerByName struct{}

func (ti taskIndexerByName) FromArgs(args ...interface{}) ([]byte, error) {
	return fromArgs(args...)
}

func (ti taskIndexerByName) FromObject(obj interface{}) (bool, []byte, error) {
	t, ok := obj.(*api.Task)
	if !ok {
		panic("unexpected type passed to FromObject")
	}

	name := naming.Task(t)

	// Add the null character as a terminator
	return true, []byte(strings.ToLower(name) + "\x00"), nil
}

func (ti taskIndexerByName) PrefixFromArgs(args ...interface{}) ([]byte, error) {
	return prefixFromArgs(args...)
}

type taskIndexerByServiceID struct{}

func (ti taskIndexerByServiceID) FromArgs(args ...interface{}) ([]byte, error) {
	return fromArgs(args...)
}

func (ti taskIndexerByServiceID) FromObject(obj interface{}) (bool, []byte, error) {
	t, ok := obj.(*api.Task)
	if !ok {
		panic("unexpected type passed to FromObject")
	}

	// Add the null character as a terminator
	val := t.ServiceID + "\x00"
	return true, []byte(val), nil
}

type taskIndexerByNodeID struct{}

func (ti taskIndexerByNodeID) FromArgs(args ...interface{}) ([]byte, error) {
	return fromArgs(args...)
}

func (ti taskIndexerByNodeID) FromObject(obj interface{}) (bool, []byte, error) {
	t, ok := obj.(*api.Task)
	if !ok {
		panic("unexpected type passed to FromObject")
	}

	// Add the null character as a terminator
	val := t.NodeID + "\x00"
	return true, []byte(val), nil
}

type taskIndexerBySlot struct{}

func (ti taskIndexerBySlot) FromArgs(args ...interface{}) ([]byte, error) {
	return fromArgs(args...)
}

func (ti taskIndexerBySlot) FromObject(obj interface{}) (bool, []byte, error) {
	t, ok := obj.(*api.Task)
	if !ok {
		panic("unexpected type passed to FromObject")
	}

	// Add the null character as a terminator
	val := t.ServiceID + "\x00" + strconv.FormatUint(t.Slot, 10) + "\x00"
	return true, []byte(val), nil
}

type taskIndexerByDesiredState struct{}

func (ti taskIndexerByDesiredState) FromArgs(args ...interface{}) ([]byte, error) {
	return fromArgs(args...)
}

func (ti taskIndexerByDesiredState) FromObject(obj interface{}) (bool, []byte, error) {
	t, ok := obj.(*api.Task)
	if !ok {
		panic("unexpected type passed to FromObject")
	}

	// Add the null character as a terminator
	return true, []byte(strconv.FormatInt(int64(t.DesiredState), 10) + "\x00"), nil
}

type taskIndexerByNetwork struct{}

func (ti taskIndexerByNetwork) FromArgs(args ...interface{}) ([]byte, error) {
	return fromArgs(args...)
}

func (ti taskIndexerByNetwork) FromObject(obj interface{}) (bool, [][]byte, error) {
	t, ok := obj.(*api.Task)
	if !ok {
		panic("unexpected type passed to FromObject")
	}

	var networkIDs [][]byte

	for _, na := range t.Spec.Networks {
		// Add the null character as a terminator
		networkIDs = append(networkIDs, []byte(na.Target+"\x00"))
	}

	return len(networkIDs) != 0, networkIDs, nil
}

type taskIndexerBySecret struct{}

func (ti taskIndexerBySecret) FromArgs(args ...interface{}) ([]byte, error) {
	return fromArgs(args...)
}

func (ti taskIndexerBySecret) FromObject(obj interface{}) (bool, [][]byte, error) {
	t, ok := obj.(*api.Task)
	if !ok {
		panic("unexpected type passed to FromObject")
	}

	container := t.Spec.GetContainer()
	if container == nil {
		return false, nil, nil
	}

	var secretIDs [][]byte

	for _, secretRef := range container.Secrets {
		// Add the null character as a terminator
		secretIDs = append(secretIDs, []byte(secretRef.SecretID+"\x00"))
	}

	return len(secretIDs) != 0, secretIDs, nil
}

type taskIndexerByTaskState struct{}

func (ts taskIndexerByTaskState) FromArgs(args ...interface{}) ([]byte, error) {
	return fromArgs(args...)
}

func (ts taskIndexerByTaskState) FromObject(obj interface{}) (bool, []byte, error) {
	t, ok := obj.(*api.Task)
	if !ok {
		panic("unexpected type passed to FromObject")
	}

	// Add the null character as a terminator
	return true, []byte(strconv.FormatInt(int64(t.Status.State), 10) + "\x00"), nil
}
