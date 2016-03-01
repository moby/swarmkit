package state

import (
	"sync"

	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/watch"
)

// MemoryStore is a concurrency-safe, in-memory implementation of the Store
// interface.
type MemoryStore struct {
	l sync.RWMutex

	nodes map[string]*api.Node
	tasks map[string]*api.Task
	jobs  map[string]*api.Job

	queue *watch.Queue
}

// NewMemoryStore returns an in-memory store.
func NewMemoryStore() WatchableStore {
	return &MemoryStore{
		nodes: make(map[string]*api.Node),
		tasks: make(map[string]*api.Task),
		jobs:  make(map[string]*api.Job),
		queue: watch.NewQueue(0),
	}
}

type readTx struct {
	s *MemoryStore
}

// View executes a read transaction.
func (s *MemoryStore) View(cb func(ReadTx) error) error {
	s.l.RLock()
	err := cb(readTx{s: s})
	s.l.RUnlock()
	return err
}

func (t readTx) Nodes() NodeSetReader {
	return nodes{s: t.s}
}

func (t readTx) Jobs() JobSetReader {
	return jobs{s: t.s}
}

func (t readTx) Tasks() TaskSetReader {
	return tasks{s: t.s}
}

type tx struct {
	s *MemoryStore
}

// Update executes a read/write transaction.
func (s *MemoryStore) Update(cb func(Tx) error) error {
	s.l.Lock()
	err := cb(tx{s: s})
	s.l.Unlock()
	return err
}

func (t tx) Nodes() NodeSet {
	return nodes{s: t.s}
}

func (t tx) Jobs() JobSet {
	return jobs{s: t.s}
}

func (t tx) Tasks() TaskSet {
	return tasks{s: t.s}
}

type nodes struct {
	s *MemoryStore
}

// Create adds a new node to the store.
// Returns ErrExist if the ID is already taken.
func (nodes nodes) Create(n *api.Node) error {
	if _, ok := nodes.s.nodes[n.Spec.ID]; ok {
		return ErrExist
	}

	nodes.s.nodes[n.Spec.ID] = n
	Publish(nodes.s.queue, EventCreateNode{Node: n})
	return nil
}

// Update updates an existing node in the store.
// Returns ErrNotExist if the node doesn't exist.
func (nodes nodes) Update(n *api.Node) error {
	if _, ok := nodes.s.nodes[n.Spec.ID]; !ok {
		return ErrNotExist
	}

	nodes.s.nodes[n.Spec.ID] = n
	Publish(nodes.s.queue, EventUpdateNode{Node: n})
	return nil
}

// Delete removes a node from the store.
// Returns ErrNotExist if the node doesn't exist.
func (nodes nodes) Delete(id string) error {
	n, ok := nodes.s.nodes[id]
	if !ok {
		return ErrNotExist
	}

	delete(nodes.s.nodes, id)
	Publish(nodes.s.queue, EventDeleteNode{Node: n})
	return nil
}

// Get looks up a node by ID.
// Returns nil if the node doesn't exist.
func (nodes nodes) Get(id string) *api.Node {
	return nodes.s.nodes[id]
}

// Find selects a set of nodes and returns them. If by is nil,
// returns all nodes.
func (nodes nodes) Find(by By) ([]*api.Node, error) {
	switch v := by.(type) {
	case all:
		return nodes.s.listNodes(), nil
	case byName:
		return nodes.s.nodesByName(string(v)), nil
	default:
		return nil, ErrInvalidFindBy
	}
}

func (s *MemoryStore) listNodes() []*api.Node {
	nodes := []*api.Node{}
	for _, n := range s.nodes {
		nodes = append(nodes, n)
	}
	return nodes
}

func (s *MemoryStore) nodesByName(name string) []*api.Node {
	//TODO(aluzzardi): This needs an index.
	nodes := []*api.Node{}
	for _, n := range s.nodes {
		if n.Spec.Meta.Name == name {
			nodes = append(nodes, n)
		}
	}
	return nodes
}

type tasks struct {
	s *MemoryStore
}

// Create adds a new task to the store.
// Returns ErrExist if the ID is already taken.
func (tasks tasks) Create(t *api.Task) error {
	if _, ok := tasks.s.tasks[t.ID]; ok {
		return ErrExist
	}

	tasks.s.tasks[t.ID] = t
	Publish(tasks.s.queue, EventCreateTask{Task: t})
	return nil
}

// Update updates an existing task in the store.
// Returns ErrNotExist if the task doesn't exist.
func (tasks tasks) Update(t *api.Task) error {
	if _, ok := tasks.s.tasks[t.ID]; !ok {
		return ErrNotExist
	}

	tasks.s.tasks[t.ID] = t
	Publish(tasks.s.queue, EventUpdateTask{Task: t})
	return nil
}

// Delete removes a task from the store.
// Returns ErrNotExist if the task doesn't exist.
func (tasks tasks) Delete(id string) error {
	t, ok := tasks.s.tasks[id]
	if !ok {
		return ErrNotExist
	}

	delete(tasks.s.tasks, id)
	Publish(tasks.s.queue, EventDeleteTask{Task: t})
	return nil
}

// Get looks up a task by ID.
// Returns nil if the task doesn't exist.
func (tasks tasks) Get(id string) *api.Task {
	return tasks.s.tasks[id]
}

// Find selects a set of tasks and returns them. If by is nil,
// returns all tasks.
func (tasks tasks) Find(by By) ([]*api.Task, error) {
	switch v := by.(type) {
	case all:
		return tasks.s.listTasks(), nil
	case byName:
		return tasks.s.tasksByName(string(v)), nil
	case byNode:
		return tasks.s.tasksByNode(string(v)), nil
	case byJob:
		return tasks.s.tasksByJob(string(v)), nil
	default:
		return nil, ErrInvalidFindBy
	}
}

// listTasks returns all tasks that are present in the store.
func (s *MemoryStore) listTasks() []*api.Task {
	tasks := []*api.Task{}
	for _, t := range s.tasks {
		tasks = append(tasks, t)
	}
	return tasks
}

// tasksByName returns the list of tasks matching a given name.
// Names are neither required nor guaranteed to be unique therefore TasksByName
// might return more than one task for a given name or no tasks at all.
func (s *MemoryStore) tasksByName(name string) []*api.Task {
	//TODO(aluzzardi): This needs an index.
	tasks := []*api.Task{}
	for _, t := range s.tasks {
		if t.Spec.Meta.Name == name {
			tasks = append(tasks, t)
		}
	}
	return tasks
}

// tasksByJob returns the list of tasks belonging to a particular Job.
func (s *MemoryStore) tasksByJob(jobID string) []*api.Task {
	//TODO(aluzzardi): This needs an index.
	tasks := []*api.Task{}
	for _, t := range s.tasks {
		if t.JobID == jobID {
			tasks = append(tasks, t)
		}
	}
	return tasks
}

// tasksByNode returns the list of tasks assigned to a particular Node.
func (s *MemoryStore) tasksByNode(nodeID string) []*api.Task {
	//TODO(aluzzardi): This needs an index.
	tasks := []*api.Task{}
	for _, t := range s.tasks {
		if t.NodeID == nodeID {
			tasks = append(tasks, t)
		}
	}
	return tasks
}

type jobs struct {
	s *MemoryStore
}

// Create adds a new job to the store.
// Returns ErrExist if the ID is already taken.
func (jobs jobs) Create(j *api.Job) error {
	if _, ok := jobs.s.jobs[j.ID]; ok {
		return ErrExist
	}

	jobs.s.jobs[j.ID] = j
	Publish(jobs.s.queue, EventCreateJob{Job: j})
	return nil
}

// Update updates an existing job in the store.
// Returns ErrNotExist if the job doesn't exist.
func (jobs jobs) Update(j *api.Job) error {
	if _, ok := jobs.s.jobs[j.ID]; !ok {
		return ErrNotExist
	}

	jobs.s.jobs[j.ID] = j
	Publish(jobs.s.queue, EventUpdateJob{Job: j})
	return nil
}

// Delete removes a job from the store.
// Returns ErrNotExist if the node doesn't exist.
func (jobs jobs) Delete(id string) error {
	j, ok := jobs.s.jobs[id]
	if !ok {
		return ErrNotExist
	}

	delete(jobs.s.jobs, id)
	Publish(jobs.s.queue, EventDeleteJob{Job: j})
	return nil
}

// Job looks up a job by ID.
// Returns nil if the job doesn't exist.
func (jobs jobs) Get(id string) *api.Job {
	return jobs.s.jobs[id]
}

// Find selects a set of jobs and returns them. If by is nil,
// returns all jobs.
func (jobs jobs) Find(by By) ([]*api.Job, error) {
	switch v := by.(type) {
	case all:
		return jobs.s.listJobs(), nil
	case byName:
		return jobs.s.jobsByName(string(v)), nil
	default:
		return nil, ErrInvalidFindBy
	}
}

// listJobs returns all jobs that are present in the store.
func (s *MemoryStore) listJobs() []*api.Job {
	jobs := []*api.Job{}
	for _, j := range s.jobs {
		jobs = append(jobs, j)
	}
	return jobs
}

// jobsByName returns the list of jobs matching a given name.
// Names are neither required nor guaranteed to be unique therefore JobsByName
// might return more than one node for a given name or no nodes at all.
func (s *MemoryStore) jobsByName(name string) []*api.Job {
	//TODO(aluzzardi): This needs an index.
	jobs := []*api.Job{}
	for _, j := range s.jobs {
		if j.Spec.Meta.Name == name {
			jobs = append(jobs, j)
		}
	}
	return jobs
}

// CopyFrom causes this store to hold a copy of the provided data set.
func (s *MemoryStore) CopyFrom(tx ReadTx) error {
	s.l.Lock()
	defer s.l.Unlock()

	// Clear existing data
	s.nodes = make(map[string]*api.Node)
	s.tasks = make(map[string]*api.Task)
	s.jobs = make(map[string]*api.Job)

	// Copy over new data
	nodes, err := tx.Nodes().Find(All)
	if err != nil {
		return err
	}
	for _, n := range nodes {
		s.nodes[n.Spec.ID] = n
	}

	tasks, err := tx.Tasks().Find(All)
	if err != nil {
		return err
	}
	for _, t := range tasks {
		s.tasks[t.ID] = t
	}

	jobs, err := tx.Jobs().Find(All)
	if err != nil {
		return err
	}
	for _, j := range jobs {
		s.jobs[j.ID] = j
	}

	return nil
}

// WatchQueue returns the publish/subscribe queue.
func (s *MemoryStore) WatchQueue() *watch.Queue {
	return s.queue
}

// Snapshot populates the provided empty store with the current items in
// this store. It then returns a watcher that is guaranteed to receive
// all events from the moment the store was forked, so the populated
// store can be kept in sync.
func (s *MemoryStore) Snapshot(storeCopier StoreCopier) (watcher chan watch.Event, err error) {
	err = s.View(func(readTx ReadTx) error {
		if err := storeCopier.CopyFrom(readTx); err != nil {
			return err
		}

		watcher = s.queue.Watch()
		return nil
	})
	return
}
