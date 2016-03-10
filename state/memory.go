package state

import (
	"fmt"
	"sync"

	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/watch"
	memdb "github.com/hashicorp/go-memdb"
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
}

// NewMemoryStore returns an in-memory store.
func NewMemoryStore() WatchableStore {
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
						Name:         indexName,
						AllowMissing: true,
						Indexer:      jobIndexerByName{},
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
		memDB: memDB,
		queue: watch.NewQueue(0),
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

	if err == nil {
		memDBTx.Commit()

		// TODO(aaronl): The memory store doesn't do anything with the
		// changelist except publish changes to the watch queue, but
		// the raft store will need to push these  changes out to the
		// raft cluster.
		for _, c := range tx.changelist {
			Publish(s.queue, c)
		}
	} else {
		memDBTx.Abort()
	}
	s.updateLock.Unlock()
	return err
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

	err := nodes.memDBTx.Insert(nodes.table(), n)
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

	err := nodes.memDBTx.Insert(nodes.table(), n)
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
			return n
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
				nodes = append(nodes, n)
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

	if n.Spec == nil || n.Spec.Meta == nil {
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

	err := tasks.memDBTx.Insert(tasks.table(), t)
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

	err := tasks.memDBTx.Insert(tasks.table(), t)
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
			return t
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
				tasks = append(tasks, t)
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

	if t.Spec == nil || t.Meta == nil {
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

// Create adds a new job to the store.
// Returns ErrExist if the ID is already taken.
func (jobs jobs) Create(j *api.Job) error {
	if jobs.Get(j.ID) != nil {
		return ErrExist
	}

	err := jobs.memDBTx.Insert(jobs.table(), j)
	if err == nil {
		jobs.tx.changelist = append(jobs.tx.changelist, EventCreateJob{Job: j})
	}
	return err
}

// Update updates an existing job in the store.
// Returns ErrNotExist if the job doesn't exist.
func (jobs jobs) Update(j *api.Job) error {
	if jobs.Get(j.ID) == nil {
		return ErrNotExist
	}

	err := jobs.memDBTx.Insert(jobs.table(), j)
	if err == nil {
		jobs.tx.changelist = append(jobs.tx.changelist, EventUpdateJob{Job: j})
	}
	return err
}

// Delete removes a job from the store.
// Returns ErrNotExist if the node doesn't exist.
func (jobs jobs) Delete(id string) error {
	j := jobs.Get(id)
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
	obj, err := jobs.memDBTx.First(jobs.table(), indexID, id)
	if err != nil {
		return nil
	}
	if obj != nil {
		if j, ok := obj.(*api.Job); ok {
			return j
		}
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
				jobs = append(jobs, j)
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
	if j.Spec == nil || j.Spec.Meta == nil {
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

	err := networks.memDBTx.Insert(networks.table(), n)
	if err == nil {
		networks.tx.changelist = append(networks.tx.changelist, EventCreateNetwork{Network: n})
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
			return n
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
				networks = append(networks, n)
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
	if n.Spec == nil || n.Spec.Meta == nil {
		return false, nil, nil
	}
	// Add the null character as a terminator
	return true, []byte(n.Spec.Meta.Name + "\x00"), nil
}

// CopyFrom causes this store to hold a copy of the provided data set.
func (s *MemoryStore) CopyFrom(readTx ReadTx) error {
	return s.Update(func(tx Tx) error {
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

// WatchQueue returns the publish/subscribe queue.
func (s *MemoryStore) WatchQueue() *watch.Queue {
	return s.queue
}
