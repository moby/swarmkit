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
						Name:         indexName,
						AllowMissing: true,
						Indexer:      networkIndexerByName{},
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

type readTx struct {
	nodes    nodes
	jobs     jobs
	tasks    tasks
	networks networks
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

type tx struct {
	nodes      nodes
	jobs       jobs
	tasks      tasks
	networks   networks
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
	}
	tx.nodes.tx = &tx
	tx.jobs.tx = &tx
	tx.tasks.tx = &tx
	tx.networks.tx = &tx

	for _, sa := range actions {
		if err := applyStoreAction(tx, sa); err != nil {
			memDBTx.Abort()
			s.updateLock.Unlock()
			return err
		}
	}

	memDBTx.Commit()

	for _, c := range tx.changelist {
		Publish(s.queue, c)
	}
	if len(tx.changelist) != 0 {
		Publish(s.queue, EventCommit{})
	}
	s.updateLock.Unlock()
	return nil
}

func applyStoreAction(tx tx, sa *api.StoreAction) error {
	switch v := sa.Action.(type) {
	case *api.StoreAction_CreateTask:
		return tx.Tasks().Create(v.CreateTask)
	case *api.StoreAction_UpdateTask:
		return tx.Tasks().Update(v.UpdateTask)
	case *api.StoreAction_RemoveTask:
		return tx.Tasks().Delete(v.RemoveTask)

	case *api.StoreAction_CreateJob:
		return tx.Jobs().Create(v.CreateJob)
	case *api.StoreAction_UpdateJob:
		return tx.Jobs().Update(v.UpdateJob)
	case *api.StoreAction_RemoveJob:
		return tx.Jobs().Delete(v.RemoveJob)

	case *api.StoreAction_CreateNetwork:
		return tx.Networks().Create(v.CreateNetwork)
	// TODO(aaronl): being added
	//case *api.StoreAction_UpdateNetwork:
	//	return tx.Networks().Update(v.UpdateNetwork)
	case *api.StoreAction_RemoveNetwork:
		return tx.Networks().Delete(v.RemoveNetwork)

	case *api.StoreAction_CreateNode:
		return tx.Nodes().Create(v.CreateNode)
	case *api.StoreAction_UpdateNode:
		return tx.Nodes().Update(v.UpdateNode)
	case *api.StoreAction_RemoveNode:
		return tx.Nodes().Delete(v.RemoveNode)
	}
	return errors.New("unrecognized action type")
}

// Update executes a read/write transaction.
func (s *MemoryStore) Update(cb func(Tx) error) error {
	s.updateLock.Lock()
	memDBTx := s.memDB.Txn(true)

	tx := tx{
		nodes:    nodes{memDBTx: memDBTx},
		jobs:     jobs{memDBTx: memDBTx},
		tasks:    tasks{memDBTx: memDBTx},
		networks: networks{memDBTx: memDBTx},
	}
	tx.nodes.tx = &tx
	tx.jobs.tx = &tx
	tx.tasks.tx = &tx
	tx.networks.tx = &tx

	err := cb(tx)

	if err == nil && s.proposer != nil {
		var sa []*api.StoreAction
		sa, err = tx.newStoreAction()

		if err == nil && sa != nil {
			err = s.proposer.ProposeValue(context.Background(), sa)
		}
	}

	if err == nil {
		memDBTx.Commit()

		for _, c := range tx.changelist {
			Publish(s.queue, c)
		}
		if len(tx.changelist) != 0 {
			Publish(s.queue, EventCommit{})
		}
	} else {
		memDBTx.Abort()
	}
	s.updateLock.Unlock()
	return err
}

func (tx tx) newStoreAction() ([]*api.StoreAction, error) {
	var actions []*api.StoreAction

	for _, c := range tx.changelist {
		var sa api.StoreAction

		switch v := c.(type) {
		case EventCreateTask:
			sa.Action = &api.StoreAction_CreateTask{
				CreateTask: v.Task,
			}
		case EventUpdateTask:
			sa.Action = &api.StoreAction_UpdateTask{
				UpdateTask: v.Task,
			}
		case EventDeleteTask:
			sa.Action = &api.StoreAction_RemoveTask{
				RemoveTask: v.Task.ID,
			}

		case EventCreateJob:
			sa.Action = &api.StoreAction_CreateJob{
				CreateJob: v.Job,
			}
		case EventUpdateJob:
			sa.Action = &api.StoreAction_UpdateJob{
				UpdateJob: v.Job,
			}
		case EventDeleteJob:
			sa.Action = &api.StoreAction_RemoveJob{
				RemoveJob: v.Job.ID,
			}

		case EventCreateNetwork:
			sa.Action = &api.StoreAction_CreateNetwork{
				CreateNetwork: v.Network,
			}
		// TODO(aaronl): being added
		/*case EventUpdateNetwork:
		sa.Action = &api.StoreAction_UpdateNetwork{
			UpdateNetwork: v.Network,
		}*/
		case EventDeleteNetwork:
			sa.Action = &api.StoreAction_RemoveNetwork{
				RemoveNetwork: v.Network.ID,
			}

		case EventCreateNode:
			sa.Action = &api.StoreAction_CreateNode{
				CreateNode: v.Node,
			}
		case EventUpdateNode:
			sa.Action = &api.StoreAction_UpdateNode{
				UpdateNode: v.Node,
			}
		case EventDeleteNode:
			sa.Action = &api.StoreAction_RemoveNode{
				RemoveNode: v.Node.ID,
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

type nodes struct {
	tx      *tx
	memDBTx *memdb.Txn
}

func (nodes nodes) table() string {
	return tableNode
}

// Create adds a new node to the store.
// Returns ErrExist if the ID is already taken.
func (nodes nodes) Create(n *api.Node) error {
	if nodes.Get(n.ID) != nil {
		return ErrExist
	}

	err := nodes.memDBTx.Insert(nodes.table(), n.Copy())
	if err == nil {
		nodes.tx.changelist = append(nodes.tx.changelist, EventCreateNode{Node: n})
	}
	return err
}

// Update updates an existing node in the store.
// Returns ErrNotExist if the node doesn't exist.
func (nodes nodes) Update(n *api.Node) error {
	if nodes.Get(n.ID) == nil {
		return ErrNotExist
	}

	err := nodes.memDBTx.Insert(nodes.table(), n.Copy())
	if err == nil {
		nodes.tx.changelist = append(nodes.tx.changelist, EventUpdateNode{Node: n})
	}
	return err
}

// Delete removes a node from the store.
// Returns ErrNotExist if the node doesn't exist.
func (nodes nodes) Delete(id string) error {
	n := nodes.Get(id)
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
	obj, err := nodes.memDBTx.First(nodes.table(), indexID, id)
	if err != nil {
		return nil
	}
	if obj != nil {
		if n, ok := obj.(*api.Node); ok {
			return n.Copy()
		}
	}
	return nil
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
	tx      *tx
	memDBTx *memdb.Txn
}

func (tasks tasks) table() string {
	return tableTask
}

// Create adds a new task to the store.
// Returns ErrExist if the ID is already taken.
func (tasks tasks) Create(t *api.Task) error {
	if tasks.Get(t.ID) != nil {
		return ErrExist
	}

	err := tasks.memDBTx.Insert(tasks.table(), t.Copy())
	if err == nil {
		tasks.tx.changelist = append(tasks.tx.changelist, EventCreateTask{Task: t})
	}
	return err
}

// Update updates an existing task in the store.
// Returns ErrNotExist if the task doesn't exist.
func (tasks tasks) Update(t *api.Task) error {
	if tasks.Get(t.ID) == nil {
		return ErrNotExist
	}

	err := tasks.memDBTx.Insert(tasks.table(), t.Copy())
	if err == nil {
		tasks.tx.changelist = append(tasks.tx.changelist, EventUpdateTask{Task: t})
	}
	return err
}

// Delete removes a task from the store.
// Returns ErrNotExist if the task doesn't exist.
func (tasks tasks) Delete(id string) error {
	t := tasks.Get(id)
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
	obj, err := tasks.memDBTx.First(tasks.table(), indexID, id)
	if err != nil {
		return nil
	}
	if obj != nil {
		if t, ok := obj.(*api.Task); ok {
			return t.Copy()
		}
	}
	return nil
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
	tx      *tx
	memDBTx *memdb.Txn
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

	err := jobs.memDBTx.Insert(jobs.table(), j.Copy())
	if err == nil {
		jobs.tx.changelist = append(jobs.tx.changelist, EventCreateJob{Job: j})
	}
	return err
}

// Update updates an existing job in the store.
// Returns ErrNotExist if the job doesn't exist.
func (jobs jobs) Update(j *api.Job) error {
	if jobs.lookup(indexID, j.ID) == nil {
		return ErrNotExist
	}
	// Ensure the name is either not in use or already used by this same Job.
	if existing := jobs.lookup(indexName, j.Spec.Meta.Name); existing != nil {
		if existing.ID != j.ID {
			return ErrNameConflict
		}
	}

	err := jobs.memDBTx.Insert(jobs.table(), j.Copy())
	if err == nil {
		jobs.tx.changelist = append(jobs.tx.changelist, EventUpdateJob{Job: j})
	}
	return err
}

// Delete removes a job from the store.
// Returns ErrNotExist if the node doesn't exist.
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
	if j := jobs.lookup(indexID, id); j != nil {
		return j.Copy()
	}
	return nil
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
	tx      *tx
	memDBTx *memdb.Txn
}

func (networks networks) table() string {
	return tableNetwork
}

// Create adds a new network to the store.
// Returns ErrExist if the ID is already taken.
func (networks networks) Create(n *api.Network) error {
	if networks.Get(n.ID) != nil {
		return ErrExist
	}

	err := networks.memDBTx.Insert(networks.table(), n.Copy())
	if err == nil {
		networks.tx.changelist = append(networks.tx.changelist, EventCreateNetwork{Network: n})
	}
	return err
}

// Update updates an existing network in the store.
// Returns ErrNotExist if the network doesn't exist.
func (networks networks) Update(n *api.Network) error {
	if networks.Get(n.ID) == nil {
		return ErrNotExist
	}

	err := networks.memDBTx.Insert(networks.table(), n.Copy())
	if err == nil {
		networks.tx.changelist = append(networks.tx.changelist, EventUpdateNetwork{Network: n})
	}
	return err
}

// Delete removes a network from the store.
// Returns ErrNotExist if the node doesn't exist.
func (networks networks) Delete(id string) error {
	n := networks.Get(id)
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
	obj, err := networks.memDBTx.First(networks.table(), indexID, id)
	if err != nil {
		return nil
	}
	if obj != nil {
		if n, ok := obj.(*api.Network); ok {
			return n.Copy()
		}
	}
	return nil
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

		return nil
	})
}

// Save serializes the data in the store.
func (s *MemoryStore) Save() ([]byte, error) {
	snapshot := pb.StoreSnapshot{
		Version: pb.StoreSnapshot_V0,
	}
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

		return nil
	})
	if err != nil {
		return nil, err
	}

	return snapshot.Marshal()
}

// Restore sets the contents of the store to the serialized data in the
// argument.
func (s *MemoryStore) Restore(data []byte) error {
	var snapshot pb.StoreSnapshot
	if err := snapshot.Unmarshal(data); err != nil {
		return err
	}

	if snapshot.Version != pb.StoreSnapshot_V0 {
		return fmt.Errorf("unrecognized snapshot version %d", snapshot.Version)
	}

	return s.Update(func(tx Tx) error {
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

		return nil
	})
}

// WatchQueue returns the publish/subscribe queue.
func (s *MemoryStore) WatchQueue() *watch.Queue {
	return s.queue
}
