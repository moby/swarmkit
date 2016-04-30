package store

import (
	"strconv"

	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/manager/state"
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
					Indexer: taskIndexerByID{},
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
				indexInstance: {
					Name:         indexInstance,
					AllowMissing: true,
					Indexer:      taskIndexerByInstance{},
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
		NewStoreAction: func(c state.Event) (api.StoreAction, error) {
			var sa api.StoreAction
			switch v := c.(type) {
			case state.EventCreateTask:
				sa.Action = api.StoreActionKindCreate
				sa.Target = &api.StoreAction_Task{
					Task: v.Task,
				}
			case state.EventUpdateTask:
				sa.Action = api.StoreActionKindUpdate
				sa.Target = &api.StoreAction_Task{
					Task: v.Task,
				}
			case state.EventDeleteTask:
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

type taskEntry struct {
	*api.Task
}

func (t taskEntry) ID() string {
	return t.Task.ID
}

func (t taskEntry) Version() api.Version {
	return t.Task.Version
}

func (t taskEntry) SetVersion(version api.Version) {
	t.Task.Version = version
}

func (t taskEntry) Copy(version *api.Version) Object {
	copy := t.Task.Copy()
	if version != nil {
		copy.Version = *version
	}
	return taskEntry{copy}
}

func (t taskEntry) EventCreate() state.Event {
	return state.EventCreateTask{Task: t.Task}
}

func (t taskEntry) EventUpdate() state.Event {
	return state.EventUpdateTask{Task: t.Task}
}

func (t taskEntry) EventDelete() state.Event {
	return state.EventDeleteTask{Task: t.Task}
}

// CreateTask adds a new task to the store.
// Returns ErrExist if the ID is already taken.
func CreateTask(tx Tx, t *api.Task) error {
	return tx.create(tableTask, taskEntry{t})
}

// UpdateTask updates an existing task in the store.
// Returns ErrNotExist if the node doesn't exist.
func UpdateTask(tx Tx, t *api.Task) error {
	return tx.update(tableTask, taskEntry{t})
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
	return t.(taskEntry).Task
}

// FindTasks selects a set of tasks and returns them.
func FindTasks(tx ReadTx, by By) ([]*api.Task, error) {
	switch by.(type) {
	case byAll, byName, byNode, byService, byInstance:
	default:
		return nil, ErrInvalidFindBy
	}

	taskList := []*api.Task{}
	err := tx.find(tableTask, by, func(o Object) {
		taskList = append(taskList, o.(taskEntry).Task)
	})
	return taskList, err
}

type taskIndexerByID struct{}

func (ti taskIndexerByID) FromArgs(args ...interface{}) ([]byte, error) {
	return fromArgs(args...)
}

func (ti taskIndexerByID) FromObject(obj interface{}) (bool, []byte, error) {
	t, ok := obj.(taskEntry)
	if !ok {
		panic("unexpected type passed to FromObject")
	}

	// Add the null character as a terminator
	val := t.Task.ID + "\x00"
	return true, []byte(val), nil
}

type taskIndexerByName struct{}

func (ti taskIndexerByName) FromArgs(args ...interface{}) ([]byte, error) {
	return fromArgs(args...)
}

func (ti taskIndexerByName) FromObject(obj interface{}) (bool, []byte, error) {
	t, ok := obj.(taskEntry)
	if !ok {
		panic("unexpected type passed to FromObject")
	}

	// Add the null character as a terminator
	return true, []byte(t.Annotations.Name + "\x00"), nil
}

type taskIndexerByServiceID struct{}

func (ti taskIndexerByServiceID) FromArgs(args ...interface{}) ([]byte, error) {
	return fromArgs(args...)
}

func (ti taskIndexerByServiceID) FromObject(obj interface{}) (bool, []byte, error) {
	t, ok := obj.(taskEntry)
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
	t, ok := obj.(taskEntry)
	if !ok {
		panic("unexpected type passed to FromObject")
	}

	// Add the null character as a terminator
	val := t.NodeID + "\x00"
	return true, []byte(val), nil
}

type taskIndexerByInstance struct{}

func (ti taskIndexerByInstance) FromArgs(args ...interface{}) ([]byte, error) {
	return fromArgs(args...)
}

func (ti taskIndexerByInstance) FromObject(obj interface{}) (bool, []byte, error) {
	t, ok := obj.(taskEntry)
	if !ok {
		panic("unexpected type passed to FromObject")
	}

	// Add the null character as a terminator
	val := t.ServiceID + "\x00" + strconv.FormatUint(t.Instance, 10) + "\x00"
	return true, []byte(val), nil
}
