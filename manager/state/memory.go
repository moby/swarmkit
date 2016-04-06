package state

import (
	"errors"
	"fmt"
	"sync"

	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/manager/state/pb"
	"github.com/docker/swarm-v2/manager/state/watch"
	memdb "github.com/hashicorp/go-memdb"
	"golang.org/x/net/context"
)

const (
	indexID     = "id"
	indexName   = "name"
	indexJobID  = "jobid"
	indexNodeID = "nodeid"

	tableNode    = "node"
	tableTask    = "task"
	tableJob     = "job"
	tableNetwork = "network"
	tableVolume  = "volume"

	prefix = "_prefix"
)

// MemoryStore is a concurrency-safe, in-memory implementation of the Store
// interface.
type MemoryStore struct {
	// updateLock must be held during an update transaction.
	updateLock sync.Mutex

	memDB *memdb.MemDB
	queue *watch.Queue

	proposer Proposer
}

// NewMemoryStore returns an in-memory store. The argument is a optional
// Proposer which will be used to propagate changes to other members in a
// cluster.
func NewMemoryStore(proposer Proposer) *MemoryStore {
	schema := &memdb.DBSchema{
		Tables: map[string]*memdb.TableSchema{
			tableNode: {
				Name: tableNode,
				Indexes: map[string]*memdb.IndexSchema{
					indexID: {
						Name:    indexID,
						Unique:  true,
						Indexer: nodeIndexerByID{},
					},
					indexName: {
						Name:         indexName,
						AllowMissing: true,
						Indexer:      nodeIndexerByName{},
					},
				},
			},
			tableTask: {
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
					indexJobID: {
						Name:         indexJobID,
						AllowMissing: true,
						Indexer:      taskIndexerByJobID{},
					},
					indexNodeID: {
						Name:         indexNodeID,
						AllowMissing: true,
						Indexer:      taskIndexerByNodeID{},
					},
				},
			},
			tableJob: {
				Name: tableJob,
				Indexes: map[string]*memdb.IndexSchema{
					indexID: {
						Name:    indexID,
						Unique:  true,
						Indexer: jobIndexerByID{},
					},
					indexName: {
						Name:    indexName,
						Unique:  true,
						Indexer: jobIndexerByName{},
					},
				},
			},
			tableNetwork: {
				Name: tableNetwork,
				Indexes: map[string]*memdb.IndexSchema{
					indexID: {
						Name:    indexID,
						Unique:  true,
						Indexer: networkIndexerByID{},
					},
					indexName: {
						Name:    indexName,
						Unique:  true,
						Indexer: networkIndexerByName{},
					},
				},
			},
			tableVolume: {
				Name: tableVolume,
				Indexes: map[string]*memdb.IndexSchema{
					indexID: {
						Name:    indexID,
						Unique:  true,
						Indexer: volumeIndexerByID{},
					},
					indexName: {
						Name:    indexName,
						Unique:  true,
						Indexer: volumeIndexerByName{},
					},
				},
			},
		},
	}

	memDB, err := memdb.NewMemDB(schema)
	if err != nil {
		// This shouldn't fail
		panic(err)
	}

	return &MemoryStore{
		memDB:    memDB,
		queue:    watch.NewQueue(0),
		proposer: proposer,
	}
}

func fromArgs(args ...interface{}) ([]byte, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("must provide only a single argument")
	}
	arg, ok := args[0].(string)
	if !ok {
		return nil, fmt.Errorf("argument must be a string: %#v", args[0])
	}
	// Add the null character as a terminator
	arg += "\x00"
	return []byte(arg), nil
}

func prefixFromArgs(args ...interface{}) ([]byte, error) {
	val, err := fromArgs(args...)
	if err != nil {
		return nil, err
	}

	// Strip the null terminator, the rest is a prefix
	n := len(val)
	if n > 0 {
		return val[:n-1], nil
	}
	return val, nil
}

type readTx struct {
	nodes    nodes
	jobs     jobs
	tasks    tasks
	networks networks
	volumes  volumes
}

// View executes a read transaction.
func (s *MemoryStore) View(cb func(ReadTx) error) error {
	memDBTx := s.memDB.Txn(false)

	readTx := readTx{
		nodes: nodes{
			memDBTx: memDBTx,
		},
		jobs: jobs{
			memDBTx: memDBTx,
		},
		tasks: tasks{
			memDBTx: memDBTx,
		},
		networks: networks{
			memDBTx: memDBTx,
		},
		volumes: volumes{
			memDBTx: memDBTx,
		},
	}
	err := cb(readTx)
	memDBTx.Commit()
	return err
}

func (t readTx) Nodes() NodeSetReader {
	return t.nodes
}

func (t readTx) Jobs() JobSetReader {
	return t.jobs
}

func (t readTx) Networks() NetworkSetReader {
	return t.networks
}

func (t readTx) Tasks() TaskSetReader {
	return t.tasks
}

func (t readTx) Volumes() VolumeSetReader {
	return t.volumes
}

type tx struct {
	nodes      nodes
	jobs       jobs
	tasks      tasks
	networks   networks
	volumes    volumes
	changelist []Event
}

// applyStoreActions updates a store based on StoreAction messages.
func (s *MemoryStore) applyStoreActions(actions []*api.StoreAction) error {
	s.updateLock.Lock()
	memDBTx := s.memDB.Txn(true)

	tx := tx{
		nodes:    nodes{memDBTx: memDBTx},
		jobs:     jobs{memDBTx: memDBTx},
		tasks:    tasks{memDBTx: memDBTx},
		networks: networks{memDBTx: memDBTx},
		volumes:  volumes{memDBTx: memDBTx},
	}
	tx.nodes.tx = &tx
	tx.jobs.tx = &tx
	tx.tasks.tx = &tx
	tx.networks.tx = &tx
	tx.volumes.tx = &tx

	for _, sa := range actions {
		if err := applyStoreAction(tx, sa); err != nil {
			memDBTx.Abort()
			s.updateLock.Unlock()
			return err
		}
	}

	memDBTx.Commit()

	for _, c := range tx.changelist {
		s.queue.Publish(c)
	}
	if len(tx.changelist) != 0 {
		s.queue.Publish(EventCommit{})
	}
	s.updateLock.Unlock()
	return nil
}

func applyStoreAction(tx tx, sa *api.StoreAction) error {
	// TODO(stevvooe): While this switch is still slightly complex, we can
	// simplify it by making the object store interface more generic. When this
	// is fixed, we can move the switch out of each branch and just set the
	// target and dataset.
	//
	// The only tricky bit is the ID. However, we can make our object implement
	// an ID interface and this goes away.

	switch v := sa.Target.(type) {
	case *api.StoreAction_Node:
		obj := v.Node
		ds := tx.Nodes()
		switch sa.Action {
		case api.StoreActionKindCreate:
			return ds.Create(obj)
		case api.StoreActionKindUpdate:
			return ds.Update(obj)
		case api.StoreActionKindRemove:
			return ds.Delete(obj.ID)
		default:
			return errors.New("unknown store action")
		}
	case *api.StoreAction_Job:
		obj := v.Job
		ds := tx.Jobs()
		switch sa.Action {
		case api.StoreActionKindCreate:
			return ds.Create(obj)
		case api.StoreActionKindUpdate:
			return ds.Update(obj)
		case api.StoreActionKindRemove:
			return ds.Delete(obj.ID)
		default:
			return errors.New("unknown store action")
		}
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
		default:
			return errors.New("unknown store action")
		}
	case *api.StoreAction_Network:
		obj := v.Network
		ds := tx.Networks()
		switch sa.Action {
		case api.StoreActionKindCreate:
			return ds.Create(obj)
		case api.StoreActionKindUpdate:
			return ds.Update(obj)
		case api.StoreActionKindRemove:
			return ds.Delete(obj.ID)
		default:
			return errors.New("unknown store action")
		}
	case *api.StoreAction_Volume:
		obj := v.Volume
		ds := tx.Volumes()
		switch sa.Action {
		case api.StoreActionKindCreate:
			return ds.Create(obj)
		case api.StoreActionKindUpdate:
			return ds.Update(obj)
		case api.StoreActionKindRemove:
			return ds.Delete(obj.ID)
		default:
			return errors.New("unknown store action")
		}
	}

	return errors.New("unrecognized action type")
}

func (s *MemoryStore) update(proposer Proposer, cb func(Tx) error) error {
	s.updateLock.Lock()
	memDBTx := s.memDB.Txn(true)

	var (
		tx         tx
		curVersion *api.Version
	)

	if proposer != nil {
		curVersion = proposer.GetVersion()
	}

	tx.nodes = nodes{
		tx:         &tx,
		memDBTx:    memDBTx,
		curVersion: curVersion,
	}
	tx.jobs = jobs{
		tx:         &tx,
		memDBTx:    memDBTx,
		curVersion: curVersion,
	}
	tx.tasks = tasks{
		tx:         &tx,
		memDBTx:    memDBTx,
		curVersion: curVersion,
	}
	tx.networks = networks{
		tx:         &tx,
		memDBTx:    memDBTx,
		curVersion: curVersion,
	}
	tx.volumes = volumes{
		tx:         &tx,
		memDBTx:    memDBTx,
		curVersion: curVersion,
	}

	err := cb(tx)

	if err == nil {
		if proposer == nil {
			memDBTx.Commit()
		} else {
			var sa []*api.StoreAction
			sa, err = tx.newStoreAction()

			if err == nil {
				if sa != nil {
					err = proposer.ProposeValue(context.Background(), sa, func() {
						memDBTx.Commit()
					})
				} else {
					memDBTx.Commit()
				}
			}
		}
	}

	if err == nil {
		for _, c := range tx.changelist {
			s.queue.Publish(c)
		}
		if len(tx.changelist) != 0 {
			s.queue.Publish(EventCommit{})
		}
	} else {
		memDBTx.Abort()
	}
	s.updateLock.Unlock()
	return err

}

func (s *MemoryStore) updateLocal(cb func(Tx) error) error {
	return s.update(nil, cb)
}

// Update executes a read/write transaction.
func (s *MemoryStore) Update(cb func(Tx) error) error {
	return s.update(s.proposer, cb)
}

func (tx tx) newStoreAction() ([]*api.StoreAction, error) {
	var actions []*api.StoreAction

	for _, c := range tx.changelist {
		var sa api.StoreAction

		// TODO(stevvooe): Refactor events to better handle this switch. Too
		// much repitition for an inner product space (CRUD x Resource).

		switch v := c.(type) {
		case EventCreateTask:
			sa.Action = api.StoreActionKindCreate
			sa.Target = &api.StoreAction_Task{
				Task: v.Task,
			}
		case EventUpdateTask:
			sa.Action = api.StoreActionKindUpdate
			sa.Target = &api.StoreAction_Task{
				Task: v.Task,
			}
		case EventDeleteTask:
			sa.Action = api.StoreActionKindRemove
			sa.Target = &api.StoreAction_Task{
				Task: v.Task,
			}

		case EventCreateJob:
			sa.Action = api.StoreActionKindCreate
			sa.Target = &api.StoreAction_Job{
				Job: v.Job,
			}
		case EventUpdateJob:
			sa.Action = api.StoreActionKindUpdate
			sa.Target = &api.StoreAction_Job{
				Job: v.Job,
			}
		case EventDeleteJob:
			sa.Action = api.StoreActionKindRemove
			sa.Target = &api.StoreAction_Job{
				Job: v.Job,
			}

		case EventCreateNetwork:
			sa.Action = api.StoreActionKindCreate
			sa.Target = &api.StoreAction_Network{
				Network: v.Network,
			}
		case EventUpdateNetwork:
			sa.Action = api.StoreActionKindUpdate
			sa.Target = &api.StoreAction_Network{
				Network: v.Network,
			}
		case EventDeleteNetwork:
			sa.Action = api.StoreActionKindRemove
			sa.Target = &api.StoreAction_Network{
				Network: v.Network,
			}

		case EventCreateNode:
			sa.Action = api.StoreActionKindCreate
			sa.Target = &api.StoreAction_Node{
				Node: v.Node,
			}
		case EventUpdateNode:
			sa.Action = api.StoreActionKindUpdate
			sa.Target = &api.StoreAction_Node{
				Node: v.Node,
			}
		case EventDeleteNode:
			sa.Action = api.StoreActionKindRemove
			sa.Target = &api.StoreAction_Node{
				Node: v.Node,
			}

		case EventCreateVolume:
			sa.Action = api.StoreActionKindCreate
			sa.Target = &api.StoreAction_Volume{
				Volume: v.Volume,
			}
		case EventUpdateVolume:
			sa.Action = api.StoreActionKindUpdate
			sa.Target = &api.StoreAction_Volume{
				Volume: v.Volume,
			}
		case EventDeleteVolume:
			sa.Action = api.StoreActionKindRemove
			sa.Target = &api.StoreAction_Volume{
				Volume: v.Volume,
			}
		default:
			return nil, errors.New("unrecognized event type")
		}
		actions = append(actions, &sa)
	}

	return actions, nil
}

func (tx tx) Nodes() NodeSet {
	return tx.nodes
}

func (tx tx) Networks() NetworkSet {
	return tx.networks
}

func (tx tx) Jobs() JobSet {
	return tx.jobs
}

func (tx tx) Tasks() TaskSet {
	return tx.tasks
}

func (tx tx) Volumes() VolumeSet {
	return tx.volumes
}

type nodes struct {
	tx         *tx
	memDBTx    *memdb.Txn
	curVersion *api.Version
}

func (nodes nodes) table() string {
	return tableNode
}

// lookup is an internal typed wrapper around memdb.
func (nodes nodes) lookup(index, id string) *api.Node {
	j, err := nodes.memDBTx.First(nodes.table(), index, id)
	if err != nil {
		return nil
	}
	if j != nil {
		return j.(*api.Node)
	}
	return nil
}

// Create adds a new node to the store.
// Returns ErrExist if the ID is already taken.
func (nodes nodes) Create(n *api.Node) error {
	if nodes.lookup(indexID, n.ID) != nil {
		return ErrExist
	}

	copy := n.Copy()
	if nodes.curVersion != nil {
		copy.Version = *nodes.curVersion
	}

	err := nodes.memDBTx.Insert(nodes.table(), copy)
	if err == nil {
		nodes.tx.changelist = append(nodes.tx.changelist, EventCreateNode{Node: copy})
	}
	return err
}

// Update updates an existing node in the store.
// Returns ErrNotExist if the node doesn't exist.
func (nodes nodes) Update(n *api.Node) error {
	oldN := nodes.lookup(indexID, n.ID)
	if oldN == nil {
		return ErrNotExist
	}

	copy := n.Copy()
	if nodes.curVersion != nil {
		if oldN.Version != n.Version {
			return ErrSequenceConflict
		}
		copy.Version = *nodes.curVersion
	}

	err := nodes.memDBTx.Insert(nodes.table(), copy)
	if err == nil {
		nodes.tx.changelist = append(nodes.tx.changelist, EventUpdateNode{Node: copy})
	}
	return err
}

// Delete removes a node from the store.
// Returns ErrNotExist if the node doesn't exist.
func (nodes nodes) Delete(id string) error {
	n := nodes.lookup(indexID, id)
	if n == nil {
		return ErrNotExist
	}

	err := nodes.memDBTx.Delete(nodes.table(), n)
	if err == nil {
		nodes.tx.changelist = append(nodes.tx.changelist, EventDeleteNode{Node: n})
	}
	return err
}

// Get looks up a node by ID.
// Returns nil if the node doesn't exist.
func (nodes nodes) Get(id string) *api.Node {
	return nodes.lookup(indexID, id).Copy()
}

// Find selects a set of nodes and returns them. If by is nil,
// returns all nodes.
func (nodes nodes) Find(by By) ([]*api.Node, error) {
	fromResultIterator := func(it memdb.ResultIterator) []*api.Node {
		nodes := []*api.Node{}
		for {
			obj := it.Next()
			if obj == nil {
				break
			}
			if n, ok := obj.(*api.Node); ok {
				nodes = append(nodes, n.Copy())
			}
		}
		return nodes
	}

	fromResultIterators := func(its ...memdb.ResultIterator) []*api.Node {
		nodes := []*api.Node{}
		ids := make(map[string]struct{})
		for _, it := range its {
			for {
				obj := it.Next()
				if obj == nil {
					break
				}
				if n, ok := obj.(*api.Node); ok {
					if _, exists := ids[n.ID]; !exists {
						nodes = append(nodes, n.Copy())
						ids[n.ID] = struct{}{}
					}
				}
			}
		}
		return nodes
	}

	switch v := by.(type) {
	case all:
		it, err := nodes.memDBTx.Get(nodes.table(), indexID)
		if err != nil {
			return nil, err
		}
		return fromResultIterator(it), nil
	case byName:
		it, err := nodes.memDBTx.Get(nodes.table(), indexName, string(v))
		if err != nil {
			return nil, err
		}
		return fromResultIterator(it), nil
	case byQuery:
		itID, err := nodes.memDBTx.Get(nodes.table(), indexID+prefix, string(v))
		if err != nil {
			return nil, err
		}
		itName, err := nodes.memDBTx.Get(nodes.table(), indexName, string(v))
		if err != nil {
			return nil, err
		}
		return fromResultIterators(itID, itName), nil
	default:
		return nil, ErrInvalidFindBy
	}
}

type nodeIndexerByID struct{}

func (ni nodeIndexerByID) FromArgs(args ...interface{}) ([]byte, error) {
	return fromArgs(args...)
}

func (ni nodeIndexerByID) FromObject(obj interface{}) (bool, []byte, error) {
	n, ok := obj.(*api.Node)
	if !ok {
		panic("unexpected type passed to FromObject")
	}

	// Add the null character as a terminator
	val := n.ID + "\x00"
	return true, []byte(val), nil
}

func (ni nodeIndexerByID) PrefixFromArgs(args ...interface{}) ([]byte, error) {
	return prefixFromArgs(args...)
}

type nodeIndexerByName struct{}

func (ni nodeIndexerByName) FromArgs(args ...interface{}) ([]byte, error) {
	return fromArgs(args...)
}

func (ni nodeIndexerByName) FromObject(obj interface{}) (bool, []byte, error) {
	n, ok := obj.(*api.Node)
	if !ok {
		panic("unexpected type passed to FromObject")
	}

	if n.Spec == nil {
		return false, nil, nil
	}
	// Add the null character as a terminator
	return true, []byte(n.Spec.Meta.Name + "\x00"), nil
}

type tasks struct {
	tx         *tx
	memDBTx    *memdb.Txn
	curVersion *api.Version
}

func (tasks tasks) table() string {
	return tableTask
}

// lookup is an internal typed wrapper around memdb.
func (tasks tasks) lookup(index, id string) *api.Task {
	j, err := tasks.memDBTx.First(tasks.table(), index, id)
	if err != nil {
		return nil
	}
	if j != nil {
		return j.(*api.Task)
	}
	return nil
}

// Create adds a new task to the store.
// Returns ErrExist if the ID is already taken.
func (tasks tasks) Create(t *api.Task) error {
	if tasks.lookup(indexID, t.ID) != nil {
		return ErrExist
	}

	copy := t.Copy()
	if tasks.curVersion != nil {
		copy.Version = *tasks.curVersion
	}

	err := tasks.memDBTx.Insert(tasks.table(), copy)
	if err == nil {
		tasks.tx.changelist = append(tasks.tx.changelist, EventCreateTask{Task: copy})
	}
	return err
}

// Update updates an existing task in the store.
// Returns ErrNotExist if the task doesn't exist.
func (tasks tasks) Update(t *api.Task) error {
	oldT := tasks.lookup(indexID, t.ID)
	if oldT == nil {
		return ErrNotExist
	}
	copy := t.Copy()
	if tasks.curVersion != nil {
		if oldT.Version != t.Version {
			return ErrSequenceConflict
		}
		copy.Version = *tasks.curVersion
	}

	err := tasks.memDBTx.Insert(tasks.table(), copy)
	if err == nil {
		tasks.tx.changelist = append(tasks.tx.changelist, EventUpdateTask{Task: copy})
	}
	return err
}

// Delete removes a task from the store.
// Returns ErrNotExist if the task doesn't exist.
func (tasks tasks) Delete(id string) error {
	t := tasks.lookup(indexID, id)
	if t == nil {
		return ErrNotExist
	}

	err := tasks.memDBTx.Delete(tasks.table(), t)
	if err == nil {
		tasks.tx.changelist = append(tasks.tx.changelist, EventDeleteTask{Task: t})
	}
	return err
}

// Get looks up a task by ID.
// Returns nil if the task doesn't exist.
func (tasks tasks) Get(id string) *api.Task {
	return tasks.lookup(indexID, id).Copy()
}

// Find selects a set of tasks and returns them. If by is nil,
// returns all tasks.
func (tasks tasks) Find(by By) ([]*api.Task, error) {
	fromResultIterator := func(it memdb.ResultIterator) []*api.Task {
		tasks := []*api.Task{}
		for {
			obj := it.Next()
			if obj == nil {
				break
			}
			if t, ok := obj.(*api.Task); ok {
				tasks = append(tasks, t.Copy())
			}
		}
		return tasks
	}
	switch v := by.(type) {
	case all:
		it, err := tasks.memDBTx.Get(tasks.table(), indexID)
		if err != nil {
			return nil, err
		}
		return fromResultIterator(it), nil
	case byName:
		it, err := tasks.memDBTx.Get(tasks.table(), indexName, string(v))
		if err != nil {
			return nil, err
		}
		return fromResultIterator(it), nil
	case byNode:
		it, err := tasks.memDBTx.Get(tasks.table(), indexNodeID, string(v))
		if err != nil {
			return nil, err
		}
		return fromResultIterator(it), nil

	case byJob:
		it, err := tasks.memDBTx.Get(tasks.table(), indexJobID, string(v))
		if err != nil {
			return nil, err
		}
		return fromResultIterator(it), nil
	default:
		return nil, ErrInvalidFindBy
	}
}

type taskIndexerByID struct{}

func (ti taskIndexerByID) FromArgs(args ...interface{}) ([]byte, error) {
	return fromArgs(args...)
}

func (ti taskIndexerByID) FromObject(obj interface{}) (bool, []byte, error) {
	t, ok := obj.(*api.Task)
	if !ok {
		panic("unexpected type passed to FromObject")
	}

	// Add the null character as a terminator
	val := t.ID + "\x00"
	return true, []byte(val), nil
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

	if t.Spec == nil {
		return false, nil, nil
	}
	// Add the null character as a terminator
	return true, []byte(t.Meta.Name + "\x00"), nil
}

type taskIndexerByJobID struct{}

func (ti taskIndexerByJobID) FromArgs(args ...interface{}) ([]byte, error) {
	return fromArgs(args...)
}

func (ti taskIndexerByJobID) FromObject(obj interface{}) (bool, []byte, error) {
	t, ok := obj.(*api.Task)
	if !ok {
		panic("unexpected type passed to FromObject")
	}

	// Add the null character as a terminator
	val := t.JobID + "\x00"
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

type jobs struct {
	tx         *tx
	memDBTx    *memdb.Txn
	curVersion *api.Version
}

func (jobs jobs) table() string {
	return tableJob
}

// lookup is an internal typed wrapper around memdb.
func (jobs jobs) lookup(index, id string) *api.Job {
	j, err := jobs.memDBTx.First(jobs.table(), index, id)
	if err != nil {
		return nil
	}
	if j != nil {
		return j.(*api.Job)
	}
	return nil
}

// Create adds a new job to the store.
// Returns ErrExist if the ID is already taken.
func (jobs jobs) Create(j *api.Job) error {
	if jobs.lookup(indexID, j.ID) != nil {
		return ErrExist
	}
	// Ensure the name is not already in use.
	if j.Spec != nil && jobs.lookup(indexName, j.Spec.Meta.Name) != nil {
		return ErrNameConflict
	}

	copy := j.Copy()
	if jobs.curVersion != nil {
		copy.Version = *jobs.curVersion
	}

	err := jobs.memDBTx.Insert(jobs.table(), copy)
	if err == nil {
		jobs.tx.changelist = append(jobs.tx.changelist, EventCreateJob{Job: copy})
	}
	return err
}

// Update updates an existing job in the store.
// Returns ErrNotExist if the job doesn't exist.
func (jobs jobs) Update(j *api.Job) error {
	oldJ := jobs.lookup(indexID, j.ID)
	if oldJ == nil {
		return ErrNotExist
	}

	// Ensure the name is either not in use or already used by this same Job.
	if existing := jobs.lookup(indexName, j.Spec.Meta.Name); existing != nil {
		if existing.ID != j.ID {
			return ErrNameConflict
		}
	}

	copy := j.Copy()
	if jobs.curVersion != nil {
		if oldJ.Version != j.Version {
			return ErrSequenceConflict
		}
		copy.Version = *jobs.curVersion
	}

	err := jobs.memDBTx.Insert(jobs.table(), copy)
	if err == nil {
		jobs.tx.changelist = append(jobs.tx.changelist, EventUpdateJob{Job: copy})
	}
	return err
}

// Delete removes a job from the store.
// Returns ErrNotExist if the job doesn't exist.
func (jobs jobs) Delete(id string) error {
	j := jobs.lookup(indexID, id)
	if j == nil {
		return ErrNotExist
	}

	err := jobs.memDBTx.Delete(jobs.table(), j)
	if err == nil {
		jobs.tx.changelist = append(jobs.tx.changelist, EventDeleteJob{Job: j})
	}
	return err
}

// Job looks up a job by ID.
// Returns nil if the job doesn't exist.
func (jobs jobs) Get(id string) *api.Job {
	return jobs.lookup(indexID, id).Copy()
}

// Find selects a set of jobs and returns them. If by is nil,
// returns all jobs.
func (jobs jobs) Find(by By) ([]*api.Job, error) {
	fromResultIterator := func(it memdb.ResultIterator) []*api.Job {
		jobs := []*api.Job{}
		for {
			obj := it.Next()
			if obj == nil {
				break
			}
			if j, ok := obj.(*api.Job); ok {
				jobs = append(jobs, j.Copy())
			}
		}
		return jobs
	}

	fromResultIterators := func(its ...memdb.ResultIterator) []*api.Job {
		jobs := []*api.Job{}
		ids := make(map[string]struct{})
		for _, it := range its {
			for {
				obj := it.Next()
				if obj == nil {
					break
				}
				if j, ok := obj.(*api.Job); ok {
					if _, exists := ids[j.ID]; !exists {
						jobs = append(jobs, j.Copy())
						ids[j.ID] = struct{}{}
					}
				}
			}
		}
		return jobs
	}

	switch v := by.(type) {
	case all:
		it, err := jobs.memDBTx.Get(jobs.table(), indexID)
		if err != nil {
			return nil, err
		}
		return fromResultIterator(it), nil
	case byName:
		it, err := jobs.memDBTx.Get(jobs.table(), indexName, string(v))
		if err != nil {
			return nil, err
		}
		return fromResultIterator(it), nil
	case byQuery:
		itID, err := jobs.memDBTx.Get(jobs.table(), indexID+prefix, string(v))
		if err != nil {
			return nil, err
		}

		itName, err := jobs.memDBTx.Get(jobs.table(), indexName, string(v))
		if err != nil {
			return nil, err
		}
		return fromResultIterators(itID, itName), nil
	default:
		return nil, ErrInvalidFindBy
	}
}

type jobIndexerByID struct{}

func (ji jobIndexerByID) FromArgs(args ...interface{}) ([]byte, error) {
	return fromArgs(args...)
}

func (ji jobIndexerByID) FromObject(obj interface{}) (bool, []byte, error) {
	j, ok := obj.(*api.Job)
	if !ok {
		panic("unexpected type passed to FromObject")
	}

	// Add the null character as a terminator
	val := j.ID + "\x00"
	return true, []byte(val), nil
}

func (ji jobIndexerByID) PrefixFromArgs(args ...interface{}) ([]byte, error) {
	return prefixFromArgs(args...)
}

type jobIndexerByName struct{}

func (ji jobIndexerByName) FromArgs(args ...interface{}) ([]byte, error) {
	return fromArgs(args...)
}

func (ji jobIndexerByName) FromObject(obj interface{}) (bool, []byte, error) {
	j, ok := obj.(*api.Job)
	if !ok {
		panic("unexpected type passed to FromObject")
	}

	// Add the null character as a terminator
	if j.Spec == nil {
		return false, nil, nil
	}
	// Add the null character as a terminator
	return true, []byte(j.Spec.Meta.Name + "\x00"), nil
}

type networks struct {
	tx         *tx
	memDBTx    *memdb.Txn
	curVersion *api.Version
}

func (networks networks) table() string {
	return tableNetwork
}

// lookup is an internal typed wrapper around memdb.
func (networks networks) lookup(index, id string) *api.Network {
	j, err := networks.memDBTx.First(networks.table(), index, id)
	if err != nil {
		return nil
	}
	if j != nil {
		return j.(*api.Network)
	}
	return nil
}

// Create adds a new network to the store.
// Returns ErrExist if the ID is already taken.
func (networks networks) Create(n *api.Network) error {
	if networks.lookup(indexID, n.ID) != nil {
		return ErrExist
	}

	// Ensure the name is not already in use.
	if n.Spec != nil && networks.lookup(indexName, n.Spec.Meta.Name) != nil {
		return ErrNameConflict
	}

	copy := n.Copy()
	if networks.curVersion != nil {
		copy.Version = *networks.curVersion
	}

	err := networks.memDBTx.Insert(networks.table(), copy)
	if err == nil {
		networks.tx.changelist = append(networks.tx.changelist, EventCreateNetwork{Network: copy})
	}
	return err
}

// Update updates an existing network in the store.
// Returns ErrNotExist if the network doesn't exist.
func (networks networks) Update(n *api.Network) error {
	oldN := networks.lookup(indexID, n.ID)
	if oldN == nil {
		return ErrNotExist
	}

	// Ensure the name is either not in use or already used by this same Network.
	if existing := networks.lookup(indexName, n.Spec.Meta.Name); existing != nil {
		if existing.ID != n.ID {
			return ErrNameConflict
		}
	}

	copy := n.Copy()
	if networks.curVersion != nil {
		if oldN.Version != n.Version {
			return ErrSequenceConflict
		}
		copy.Version = *networks.curVersion
	}

	err := networks.memDBTx.Insert(networks.table(), copy)
	if err == nil {
		networks.tx.changelist = append(networks.tx.changelist, EventUpdateNetwork{Network: copy})
	}
	return err
}

// Delete removes a network from the store.
// Returns ErrNotExist if the node doesn't exist.
func (networks networks) Delete(id string) error {
	n := networks.lookup(indexID, id)
	if n == nil {
		return ErrNotExist
	}

	err := networks.memDBTx.Delete(networks.table(), n)
	if err == nil {
		networks.tx.changelist = append(networks.tx.changelist, EventDeleteNetwork{Network: n})
	}
	return err
}

// Get looks up a network by ID.
// Returns nil if the network doesn't exist.
func (networks networks) Get(id string) *api.Network {
	return networks.lookup(indexID, id).Copy()
}

// Find selects a set of networks and returns them. If by is nil,
// returns all networks.
func (networks networks) Find(by By) ([]*api.Network, error) {
	fromResultIterator := func(it memdb.ResultIterator) []*api.Network {
		networks := []*api.Network{}
		for {
			obj := it.Next()
			if obj == nil {
				break
			}
			if n, ok := obj.(*api.Network); ok {
				networks = append(networks, n.Copy())
			}
		}
		return networks
	}

	fromResultIterators := func(its ...memdb.ResultIterator) []*api.Network {
		networks := []*api.Network{}
		ids := make(map[string]struct{})
		for _, it := range its {
			for {
				obj := it.Next()
				if obj == nil {
					break
				}
				if n, ok := obj.(*api.Network); ok {
					if _, exists := ids[n.ID]; !exists {
						networks = append(networks, n.Copy())
						ids[n.ID] = struct{}{}
					}
				}
			}
		}
		return networks
	}

	switch v := by.(type) {
	case all:
		it, err := networks.memDBTx.Get(networks.table(), indexID)
		if err != nil {
			return nil, err
		}
		return fromResultIterator(it), nil
	case byName:
		it, err := networks.memDBTx.Get(networks.table(), indexName, string(v))
		if err != nil {
			return nil, err
		}
		return fromResultIterator(it), nil
	case byQuery:
		itID, err := networks.memDBTx.Get(networks.table(), indexID+prefix, string(v))
		if err != nil {
			return nil, err
		}

		itName, err := networks.memDBTx.Get(networks.table(), indexName, string(v))
		if err != nil {
			return nil, err
		}
		return fromResultIterators(itID, itName), nil
	default:
		return nil, ErrInvalidFindBy
	}

}

type networkIndexerByID struct{}

func (ni networkIndexerByID) FromArgs(args ...interface{}) ([]byte, error) {
	return fromArgs(args...)
}

func (ni networkIndexerByID) FromObject(obj interface{}) (bool, []byte, error) {
	n, ok := obj.(*api.Network)
	if !ok {
		panic("unexpected type passed to FromObject")
	}

	// Add the null character as a terminator
	val := n.ID + "\x00"
	return true, []byte(val), nil
}

func (ni networkIndexerByID) PrefixFromArgs(args ...interface{}) ([]byte, error) {
	return prefixFromArgs(args...)
}

type networkIndexerByName struct{}

func (ni networkIndexerByName) FromArgs(args ...interface{}) ([]byte, error) {
	return fromArgs(args...)
}

func (ni networkIndexerByName) FromObject(obj interface{}) (bool, []byte, error) {
	n, ok := obj.(*api.Network)
	if !ok {
		panic("unexpected type passed to FromObject")
	}

	// Add the null character as a terminator
	if n.Spec == nil {
		return false, nil, nil
	}
	// Add the null character as a terminator
	return true, []byte(n.Spec.Meta.Name + "\x00"), nil
}

type volumes struct {
	tx         *tx
	memDBTx    *memdb.Txn
	curVersion *api.Version
}

func (volumes volumes) table() string {
	return tableVolume
}

// lookup is an internal typed wrapper around memdb.
func (volumes volumes) lookup(index, id string) *api.Volume {
	v, err := volumes.memDBTx.First(volumes.table(), index, id)
	if err != nil {
		return nil
	}
	if v != nil {
		return v.(*api.Volume)
	}
	return nil
}

// Create adds a new volume to the store.
// Returns ErrExist if the ID is already taken.
func (volumes volumes) Create(v *api.Volume) error {
	if volumes.lookup(indexID, v.ID) != nil {
		return ErrExist
	}

	// Ensure the name is not already in use.
	if v.Spec != nil && volumes.lookup(indexName, v.Spec.Meta.Name) != nil {
		return ErrNameConflict
	}

	copy := v.Copy()
	if volumes.curVersion != nil {
		copy.Version = *volumes.curVersion
	}

	err := volumes.memDBTx.Insert(volumes.table(), copy)
	if err == nil {
		volumes.tx.changelist = append(volumes.tx.changelist, EventCreateVolume{Volume: copy})
	}
	return err
}

// Update updates an existing volume in the store.
// Returns ErrNotExist if the volume doesn't exist.
func (volumes volumes) Update(v *api.Volume) error {
	if volumes.lookup(indexID, v.ID) == nil {
		return ErrNotExist
	}

	// Ensure the name is either not in use or already used by this same Volume.
	if existing := volumes.lookup(indexName, v.Spec.Meta.Name); existing != nil {
		if existing.ID != v.ID {
			return ErrNameConflict
		}
	}

	copy := v.Copy()
	if volumes.curVersion != nil {
		copy.Version = *volumes.curVersion
	}

	err := volumes.memDBTx.Insert(volumes.table(), copy)
	if err == nil {
		volumes.tx.changelist = append(volumes.tx.changelist, EventUpdateVolume{Volume: copy})
	}
	return err
}

// Delete removes a volume from the store.
// Returns ErrNotExist if the volume doesn't exist.
func (volumes volumes) Delete(id string) error {
	v := volumes.lookup(indexID, id)
	if v == nil {
		return ErrNotExist
	}

	err := volumes.memDBTx.Delete(volumes.table(), v)
	if err == nil {
		volumes.tx.changelist = append(volumes.tx.changelist, EventDeleteVolume{Volume: v})
	}
	return err
}

// Get looks up a volume by ID.
// Returns nil if the volume doesn't exist.
func (volumes volumes) Get(id string) *api.Volume {
	if v := volumes.lookup(indexID, id); v != nil {
		return v.Copy()
	}
	return nil
}

// Find selects a set of volumes and returns them. If by is nil,
// returns all volumes.
func (volumes volumes) Find(by By) ([]*api.Volume, error) {
	fromResultIterator := func(it memdb.ResultIterator) []*api.Volume {
		volumes := []*api.Volume{}
		for {
			obj := it.Next()
			if obj == nil {
				break
			}
			if v, ok := obj.(*api.Volume); ok {
				volumes = append(volumes, v.Copy())
			}
		}
		return volumes
	}
	switch c := by.(type) {
	case all:
		it, err := volumes.memDBTx.Get(volumes.table(), indexID)
		if err != nil {
			return nil, err
		}
		return fromResultIterator(it), nil
	case byName:
		it, err := volumes.memDBTx.Get(volumes.table(), indexName, string(c))
		if err != nil {
			return nil, err
		}
		return fromResultIterator(it), nil
	default:
		return nil, ErrInvalidFindBy
	}

}

type volumeIndexerByID struct{}

func (vi volumeIndexerByID) FromArgs(args ...interface{}) ([]byte, error) {
	return fromArgs(args...)
}

func (vi volumeIndexerByID) FromObject(obj interface{}) (bool, []byte, error) {
	v, ok := obj.(*api.Volume)
	if !ok {
		panic("unexpected type passed to FromObject")
	}

	// Add the null character as a terminator
	val := v.ID + "\x00"
	return true, []byte(val), nil
}

type volumeIndexerByName struct{}

func (vi volumeIndexerByName) FromArgs(args ...interface{}) ([]byte, error) {
	return fromArgs(args...)
}

func (vi volumeIndexerByName) FromObject(obj interface{}) (bool, []byte, error) {
	v, ok := obj.(*api.Volume)
	if !ok {
		panic("unexpected type passed to FromObject")
	}

	// Add the null character as a terminator
	if v.Spec == nil {
		return false, nil, nil
	}
	// Add the null character as a terminator
	return true, []byte(v.Spec.Meta.Name + "\x00"), nil
}

// CopyFrom causes this store to hold a copy of the provided data set.
func (s *MemoryStore) CopyFrom(readTx ReadTx) error {
	return s.Update(func(tx Tx) error {
		if err := DeleteAll(tx); err != nil {
			return err
		}

		// Copy over new data
		nodes, err := readTx.Nodes().Find(All)
		if err != nil {
			return err
		}
		for _, n := range nodes {
			if err := tx.Nodes().Create(n); err != nil {
				return err
			}
		}

		tasks, err := readTx.Tasks().Find(All)
		if err != nil {
			return err
		}
		for _, t := range tasks {
			if err := tx.Tasks().Create(t); err != nil {
				return err
			}
		}

		jobs, err := readTx.Jobs().Find(All)
		if err != nil {
			return err
		}
		for _, j := range jobs {
			if err := tx.Jobs().Create(j); err != nil {
				return err
			}
		}

		networks, err := readTx.Networks().Find(All)
		if err != nil {
			return err
		}
		for _, n := range networks {
			if err := tx.Networks().Create(n); err != nil {
				return err
			}
		}

		volumes, err := readTx.Volumes().Find(All)
		if err != nil {
			return err
		}
		for _, v := range volumes {
			if err := tx.Volumes().Create(v); err != nil {
				return err
			}
		}

		return nil
	})
}

// Save serializes the data in the store.
func (s *MemoryStore) Save() (*pb.StoreSnapshot, error) {
	var snapshot pb.StoreSnapshot
	err := s.View(func(tx ReadTx) error {
		var err error
		snapshot.Nodes, err = tx.Nodes().Find(All)
		if err != nil {
			return err
		}
		snapshot.Tasks, err = tx.Tasks().Find(All)
		if err != nil {
			return err
		}
		snapshot.Networks, err = tx.Networks().Find(All)
		if err != nil {
			return err
		}
		snapshot.Jobs, err = tx.Jobs().Find(All)
		if err != nil {
			return err
		}
		snapshot.Volumes, err = tx.Volumes().Find(All)
		if err != nil {
			return err
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	return &snapshot, nil
}

// Restore sets the contents of the store to the serialized data in the
// argument.
func (s *MemoryStore) Restore(snapshot *pb.StoreSnapshot) error {
	return s.updateLocal(func(tx Tx) error {
		if err := DeleteAll(tx); err != nil {
			return err
		}

		for _, n := range snapshot.Nodes {
			if err := tx.Nodes().Create(n); err != nil {
				return err
			}
		}

		for _, j := range snapshot.Jobs {
			if err := tx.Jobs().Create(j); err != nil {
				return err
			}
		}

		for _, n := range snapshot.Networks {
			if err := tx.Networks().Create(n); err != nil {
				return err
			}
		}

		for _, t := range snapshot.Tasks {
			if err := tx.Tasks().Create(t); err != nil {
				return err
			}
		}

		for _, t := range snapshot.Volumes {
			if err := tx.Volumes().Create(t); err != nil {
				return err
			}
		}

		return nil
	})
}

// WatchQueue returns the publish/subscribe queue.
func (s *MemoryStore) WatchQueue() *watch.Queue {
	return s.queue
}
