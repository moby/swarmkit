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
		Save: func(tx state.ReadTx, snapshot *api.StoreSnapshot) error {
			var err error
			snapshot.Tasks, err = tx.Tasks().Find(state.All)
			return err
		},
		Restore: func(tx state.Tx, snapshot *api.StoreSnapshot) error {
			tasks, err := tx.Tasks().Find(state.All)
			if err != nil {
				return err
			}
			for _, t := range tasks {
				if err := tx.Tasks().Delete(t.ID); err != nil {
					return err
				}
			}
			for _, t := range snapshot.Tasks {
				if err := tx.Tasks().Create(t); err != nil {
					return err
				}
			}
			return nil
		},
		ApplyStoreAction: func(tx state.Tx, sa *api.StoreAction) error {
			switch v := sa.Target.(type) {
			case *api.StoreAction_Task:
				obj := v.Task
				ds := tx.Tasks()
				switch sa.Action {
				case api.StoreActionKindCreate:
					return ds.Create(obj)
				case api.StoreActionKindUpdate:
					return ds.Update(obj)
				case api.StoreActionKindRemove:
					return ds.Delete(obj.ID)
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

func (t taskEntry) Copy(version *api.Version) state.Object {
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

type tasks struct {
	tx      *tx
	memDBTx *memdb.Txn
}

func (tasks tasks) table() string {
	return tableTask
}

// Create adds a new task to the store.
// Returns ErrExist if the ID is already taken.
func (tasks tasks) Create(t *api.Task) error {
	err := tasks.tx.create(tasks.table(), taskEntry{t})
	if err == nil && tasks.tx.curVersion != nil {
		t.Version = *tasks.tx.curVersion
	}
	return err
}

// Update updates an existing task in the store.
// Returns ErrNotExist if the node doesn't exist.
func (tasks tasks) Update(t *api.Task) error {
	return tasks.tx.update(tasks.table(), taskEntry{t})
}

// Delete removes a task from the store.
// Returns ErrNotExist if the task doesn't exist.
func (tasks tasks) Delete(id string) error {
	return tasks.tx.delete(tasks.table(), id)
}

// Get looks up a task by ID.
// Returns nil if the task doesn't exist.
func (tasks tasks) Get(id string) *api.Task {
	t := get(tasks.memDBTx, tasks.table(), id)
	if t == nil {
		return nil
	}
	return t.(taskEntry).Task
}

// Find selects a set of tasks and returns them.
func (tasks tasks) Find(by state.By) ([]*api.Task, error) {
	switch by.(type) {
	case state.AllFinder, state.NameFinder, state.NodeFinder, state.ServiceFinder, state.InstanceFinder:
	default:
		return nil, state.ErrInvalidFindBy
	}

	taskList := []*api.Task{}
	err := find(tasks.memDBTx, tasks.table(), by, func(o state.Object) {
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
	return true, []byte(t.ServiceAnnotations.Name + "\x00"), nil
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
