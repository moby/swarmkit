package state

import (
	"sync"

	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/watch"
)

// MemoryStore is a concurrency-safe, in-memory implementation of the Store
// interface.
type MemoryStore struct {
	// The read half of l must be held while reading the maps below.
	// The write half of l must be held while changing the maps below.
	l sync.RWMutex
	// updateLock must be held during an update transaction. It must be
	// acquired before the write half of l.
	updateLock sync.Mutex

	nodes    map[string]*api.Node
	tasks    map[string]*api.Task
	jobs     map[string]*api.Job
	networks map[string]*api.Network

	queue *watch.Queue
}

// NewMemoryStore returns an in-memory store.
func NewMemoryStore() WatchableStore {
	return &MemoryStore{
		nodes:    make(map[string]*api.Node),
		tasks:    make(map[string]*api.Task),
		jobs:     make(map[string]*api.Job),
		networks: make(map[string]*api.Network),
		queue:    watch.NewQueue(0),
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

func (t readTx) Networks() NetworkSetReader {
	return networks{s: t.s}
}

func (t readTx) Tasks() TaskSetReader {
	return tasks{s: t.s}
}

type tx struct {
	s          *MemoryStore
	nodes      nodes
	jobs       jobs
	tasks      tasks
	changelist []Event
}

// Update executes a read/write transaction.
func (s *MemoryStore) Update(cb func(Tx) error) error {
	s.updateLock.Lock()
	s.l.RLock()
	tx := tx{s: s}
	tx.nodes = nodes{
		s:       s,
		tx:      &tx,
		updated: make(map[string]*api.Node),
		deleted: make(map[string]struct{}),
	}
	tx.jobs = jobs{
		s:       s,
		tx:      &tx,
		updated: make(map[string]*api.Job),
		deleted: make(map[string]struct{}),
	}
	tx.tasks = tasks{
		s:       s,
		tx:      &tx,
		updated: make(map[string]*api.Task),
		deleted: make(map[string]struct{}),
	}
	err := cb(tx)
	s.l.RUnlock()

	if err == nil {
		s.l.Lock()
		err = tx.commitChangelist()
		s.l.Unlock()
	}
	s.updateLock.Unlock()
	return err
}

func (tx tx) commitChangelist() error {
	for _, c := range tx.changelist {
		switch v := c.(type) {
		case EventCreateTask:
			tx.s.tasks[v.Task.ID] = v.Task
		case EventUpdateTask:
			tx.s.tasks[v.Task.ID] = v.Task
		case EventDeleteTask:
			delete(tx.s.tasks, v.Task.ID)

		case EventCreateJob:
			tx.s.jobs[v.Job.ID] = v.Job
		case EventUpdateJob:
			tx.s.jobs[v.Job.ID] = v.Job
		case EventDeleteJob:
			delete(tx.s.jobs, v.Job.ID)

		case EventCreateNode:
			tx.s.nodes[v.Node.ID] = v.Node
		case EventUpdateNode:
			tx.s.nodes[v.Node.ID] = v.Node
		case EventDeleteNode:
			delete(tx.s.nodes, v.Node.ID)
		}
		Publish(tx.s.queue, c)
	}
	return nil
}

func (tx tx) Nodes() NodeSet {
	return tx.nodes
}

func (t tx) Networks() NetworkSet {
	return networks{s: t.s}
}

func (tx tx) Jobs() JobSet {
	return tx.jobs
}

func (tx tx) Tasks() TaskSet {
	return tx.tasks
}

type nodes struct {
	s       *MemoryStore
	tx      *tx
	updated map[string]*api.Node
	deleted map[string]struct{}
}

// Create adds a new node to the store.
// Returns ErrExist if the ID is already taken.
func (nodes nodes) Create(n *api.Node) error {
	if _, ok := nodes.deleted[n.ID]; !ok {
		if _, ok := nodes.s.nodes[n.ID]; ok {
			return ErrExist
		}
		if _, ok := nodes.updated[n.ID]; ok {
			return ErrExist
		}
	} else {
		delete(nodes.deleted, n.ID)
	}

	nodes.updated[n.ID] = n
	nodes.tx.changelist = append(nodes.tx.changelist, EventCreateNode{Node: n})
	return nil
}

// Update updates an existing node in the store.
// Returns ErrNotExist if the node doesn't exist.
func (nodes nodes) Update(n *api.Node) error {
	if _, ok := nodes.deleted[n.ID]; ok {
		return ErrNotExist
	}
	if _, ok := nodes.updated[n.ID]; !ok {
		if _, ok := nodes.s.nodes[n.ID]; !ok {
			return ErrNotExist
		}
	}

	nodes.updated[n.ID] = n
	nodes.tx.changelist = append(nodes.tx.changelist, EventUpdateNode{Node: n})
	return nil
}

// Delete removes a node from the store.
// Returns ErrNotExist if the node doesn't exist.
func (nodes nodes) Delete(id string) error {
	if _, ok := nodes.deleted[id]; ok {
		return ErrNotExist
	}
	n, ok := nodes.updated[id]
	if !ok {
		n, ok = nodes.s.nodes[id]
		if !ok {
			return ErrNotExist
		}
	} else {
		delete(nodes.updated, id)
	}

	nodes.deleted[id] = struct{}{}
	nodes.tx.changelist = append(nodes.tx.changelist, EventDeleteNode{Node: n})
	return nil
}

// Get looks up a node by ID.
// Returns nil if the node doesn't exist.
func (nodes nodes) Get(id string) *api.Node {
	if _, ok := nodes.deleted[id]; ok {
		return nil
	}
	if n, ok := nodes.updated[id]; ok {
		return n
	}
	return nodes.s.nodes[id]
}

// Find selects a set of nodes and returns them. If by is nil,
// returns all nodes.
func (nodes nodes) Find(by By) ([]*api.Node, error) {
	switch v := by.(type) {
	case all:
		return nodes.s.listNodes(nodes.deleted, nodes.updated), nil
	case byName:
		return nodes.s.nodesByName(string(v), nodes.deleted, nodes.updated), nil
	default:
		return nil, ErrInvalidFindBy
	}
}

func (s *MemoryStore) listNodes(deleted map[string]struct{}, updated map[string]*api.Node) []*api.Node {
	nodes := []*api.Node{}
	for _, n := range s.nodes {
		if deleted != nil {
			if _, ok := deleted[n.ID]; ok {
				continue
			}
		}
		if updated != nil {
			if _, ok := updated[n.ID]; ok {
				continue
			}
		}
		nodes = append(nodes, n)
	}
	if updated != nil {
		for _, n := range updated {
			nodes = append(nodes, n)
		}
	}
	return nodes
}

func (s *MemoryStore) nodesByName(name string, deleted map[string]struct{}, updated map[string]*api.Node) []*api.Node {
	//TODO(aluzzardi): This needs an index.
	nodes := []*api.Node{}
	for _, n := range s.nodes {
		if n.Spec.Meta.Name == name {
			if deleted != nil {
				if _, ok := deleted[n.ID]; ok {
					continue
				}
			}
			if updated != nil {
				if _, ok := updated[n.ID]; ok {
					continue
				}
			}

			nodes = append(nodes, n)
		}
	}
	if updated != nil {
		for _, n := range updated {
			if n.Spec.Meta.Name == name {
				nodes = append(nodes, n)
			}
		}
	}
	return nodes
}

type tasks struct {
	s       *MemoryStore
	tx      *tx
	updated map[string]*api.Task
	deleted map[string]struct{}
}

// Create adds a new task to the store.
// Returns ErrExist if the ID is already taken.
func (tasks tasks) Create(t *api.Task) error {
	if _, ok := tasks.deleted[t.ID]; !ok {
		if _, ok := tasks.s.tasks[t.ID]; ok {
			return ErrExist
		}
		if _, ok := tasks.updated[t.ID]; ok {
			return ErrExist
		}
	} else {
		delete(tasks.deleted, t.ID)
	}

	tasks.updated[t.ID] = t
	tasks.tx.changelist = append(tasks.tx.changelist, EventCreateTask{Task: t})
	return nil
}

// Update updates an existing task in the store.
// Returns ErrNotExist if the task doesn't exist.
func (tasks tasks) Update(t *api.Task) error {
	if _, ok := tasks.deleted[t.ID]; ok {
		return ErrNotExist
	}
	if _, ok := tasks.updated[t.ID]; !ok {
		if _, ok := tasks.s.tasks[t.ID]; !ok {
			return ErrNotExist
		}
	}

	tasks.updated[t.ID] = t
	tasks.tx.changelist = append(tasks.tx.changelist, EventUpdateTask{Task: t})
	return nil
}

// Delete removes a task from the store.
// Returns ErrNotExist if the task doesn't exist.
func (tasks tasks) Delete(id string) error {
	if _, ok := tasks.deleted[id]; ok {
		return ErrNotExist
	}
	t, ok := tasks.updated[id]
	if !ok {
		t, ok = tasks.s.tasks[id]
		if !ok {
			return ErrNotExist
		}
	} else {
		delete(tasks.updated, id)
	}

	tasks.deleted[id] = struct{}{}
	tasks.tx.changelist = append(tasks.tx.changelist, EventDeleteTask{Task: t})
	return nil
}

// Get looks up a task by ID.
// Returns nil if the task doesn't exist.
func (tasks tasks) Get(id string) *api.Task {
	if _, ok := tasks.deleted[id]; ok {
		return nil
	}
	if n, ok := tasks.updated[id]; ok {
		return n
	}
	return tasks.s.tasks[id]
}

// Find selects a set of tasks and returns them. If by is nil,
// returns all tasks.
func (tasks tasks) Find(by By) ([]*api.Task, error) {
	switch v := by.(type) {
	case all:
		return tasks.s.listTasks(tasks.deleted, tasks.updated), nil
	case byName:
		return tasks.s.tasksByName(string(v), tasks.deleted, tasks.updated), nil
	case byNode:
		return tasks.s.tasksByNode(string(v), tasks.deleted, tasks.updated), nil
	case byJob:
		return tasks.s.tasksByJob(string(v), tasks.deleted, tasks.updated), nil
	default:
		return nil, ErrInvalidFindBy
	}
}

// listTasks returns all tasks that are present in the store.
func (s *MemoryStore) listTasks(deleted map[string]struct{}, updated map[string]*api.Task) []*api.Task {
	tasks := []*api.Task{}
	for _, t := range s.tasks {
		if deleted != nil {
			if _, ok := deleted[t.ID]; ok {
				continue
			}
		}
		if updated != nil {
			if _, ok := updated[t.ID]; ok {
				continue
			}
		}
		tasks = append(tasks, t)
	}
	if updated != nil {
		for _, t := range updated {
			tasks = append(tasks, t)
		}
	}
	return tasks
}

// tasksByName returns the list of tasks matching a given name.
// Names are neither required nor guaranteed to be unique therefore TasksByName
// might return more than one task for a given name or no tasks at all.
func (s *MemoryStore) tasksByName(name string, deleted map[string]struct{}, updated map[string]*api.Task) []*api.Task {
	//TODO(aluzzardi): This needs an index.
	tasks := []*api.Task{}
	for _, t := range s.tasks {
		if t.Spec.Meta.Name == name {
			if deleted != nil {
				if _, ok := deleted[t.ID]; ok {
					continue
				}
			}
			if updated != nil {
				if _, ok := updated[t.ID]; ok {
					continue
				}
			}

			tasks = append(tasks, t)
		}
	}
	if updated != nil {
		for _, t := range updated {
			if t.Spec.Meta.Name == name {
				tasks = append(tasks, t)
			}
		}
	}
	return tasks
}

// tasksByJob returns the list of tasks belonging to a particular Job.
func (s *MemoryStore) tasksByJob(jobID string, deleted map[string]struct{}, updated map[string]*api.Task) []*api.Task {
	//TODO(aluzzardi): This needs an index.
	tasks := []*api.Task{}
	for _, t := range s.tasks {
		if t.JobID == jobID {
			if deleted != nil {
				if _, ok := deleted[t.ID]; ok {
					continue
				}
			}
			if updated != nil {
				if _, ok := updated[t.ID]; ok {
					continue
				}
			}

			tasks = append(tasks, t)
		}
	}
	if updated != nil {
		for _, t := range updated {
			if t.JobID == jobID {
				tasks = append(tasks, t)
			}
		}
	}
	return tasks
}

// tasksByNode returns the list of tasks assigned to a particular Node.
func (s *MemoryStore) tasksByNode(nodeID string, deleted map[string]struct{}, updated map[string]*api.Task) []*api.Task {
	//TODO(aluzzardi): This needs an index.
	tasks := []*api.Task{}
	for _, t := range s.tasks {
		if t.NodeID == nodeID {
			if deleted != nil {
				if _, ok := deleted[t.ID]; ok {
					continue
				}
			}
			if updated != nil {
				if _, ok := updated[t.ID]; ok {
					continue
				}
			}

			tasks = append(tasks, t)
		}
	}
	if updated != nil {
		for _, t := range updated {
			if t.NodeID == nodeID {
				tasks = append(tasks, t)
			}
		}
	}
	return tasks
}

type jobs struct {
	s       *MemoryStore
	tx      *tx
	updated map[string]*api.Job
	deleted map[string]struct{}
}

// Create adds a new job to the store.
// Returns ErrExist if the ID is already taken.
func (jobs jobs) Create(j *api.Job) error {
	if _, ok := jobs.deleted[j.ID]; !ok {
		if _, ok := jobs.s.jobs[j.ID]; ok {
			return ErrExist
		}
		if _, ok := jobs.updated[j.ID]; ok {
			return ErrExist
		}
	} else {
		delete(jobs.deleted, j.ID)
	}

	jobs.updated[j.ID] = j
	jobs.tx.changelist = append(jobs.tx.changelist, EventCreateJob{Job: j})
	return nil
}

// Update updates an existing job in the store.
// Returns ErrNotExist if the job doesn't exist.
func (jobs jobs) Update(j *api.Job) error {
	if _, ok := jobs.deleted[j.ID]; ok {
		return ErrNotExist
	}
	if _, ok := jobs.updated[j.ID]; !ok {
		if _, ok := jobs.s.jobs[j.ID]; !ok {
			return ErrNotExist
		}
	}

	jobs.updated[j.ID] = j
	jobs.tx.changelist = append(jobs.tx.changelist, EventUpdateJob{Job: j})
	return nil
}

// Delete removes a job from the store.
// Returns ErrNotExist if the node doesn't exist.
func (jobs jobs) Delete(id string) error {
	if _, ok := jobs.deleted[id]; ok {
		return ErrNotExist
	}
	j, ok := jobs.updated[id]
	if !ok {
		j, ok = jobs.s.jobs[id]
		if !ok {
			return ErrNotExist
		}
	} else {
		delete(jobs.updated, id)
	}

	jobs.deleted[id] = struct{}{}
	jobs.tx.changelist = append(jobs.tx.changelist, EventDeleteJob{Job: j})
	return nil

}

// Job looks up a job by ID.
// Returns nil if the job doesn't exist.
func (jobs jobs) Get(id string) *api.Job {
	if _, ok := jobs.deleted[id]; ok {
		return nil
	}
	if n, ok := jobs.updated[id]; ok {
		return n
	}
	return jobs.s.jobs[id]
}

// Find selects a set of jobs and returns them. If by is nil,
// returns all jobs.
func (jobs jobs) Find(by By) ([]*api.Job, error) {
	switch v := by.(type) {
	case all:
		return jobs.s.listJobs(jobs.deleted, jobs.updated), nil
	case byName:
		return jobs.s.jobsByName(string(v), jobs.deleted, jobs.updated), nil
	default:
		return nil, ErrInvalidFindBy
	}
}

// listJobs returns all jobs that are present in the store.
func (s *MemoryStore) listJobs(deleted map[string]struct{}, updated map[string]*api.Job) []*api.Job {
	jobs := []*api.Job{}
	for _, j := range s.jobs {
		if deleted != nil {
			if _, ok := deleted[j.ID]; ok {
				continue
			}
		}
		if updated != nil {
			if _, ok := updated[j.ID]; ok {
				continue
			}
		}
		jobs = append(jobs, j)
	}
	if updated != nil {
		for _, j := range updated {
			jobs = append(jobs, j)
		}
	}
	return jobs
}

// jobsByName returns the list of jobs matching a given name.
// Names are neither required nor guaranteed to be unique therefore JobsByName
// might return more than one node for a given name or no nodes at all.
func (s *MemoryStore) jobsByName(name string, deleted map[string]struct{}, updated map[string]*api.Job) []*api.Job {
	//TODO(aluzzardi): This needs an index.
	jobs := []*api.Job{}
	for _, j := range s.jobs {
		if j.Spec.Meta.Name == name {
			if deleted != nil {
				if _, ok := deleted[j.ID]; ok {
					continue
				}
			}
			if updated != nil {
				if _, ok := updated[j.ID]; ok {
					continue
				}
			}

			jobs = append(jobs, j)
		}
	}
	if updated != nil {
		for _, j := range updated {
			if j.Spec.Meta.Name == name {
				jobs = append(jobs, j)
			}
		}
	}
	return jobs
}

type networks struct {
	s *MemoryStore
}

// Create adds a new network to the store.
// Returns ErrExist if the ID is already taken.
func (networks networks) Create(n *api.Network) error {
	if _, ok := networks.s.networks[n.ID]; ok {
		return ErrExist
	}

	networks.s.networks[n.ID] = n
	Publish(networks.s.queue, EventCreateNetwork{Network: n})
	return nil
}

// Delete removes a network from the store.
// Returns ErrNotExist if the node doesn't exist.
func (networks networks) Delete(id string) error {
	n, ok := networks.s.networks[id]
	if !ok {
		return ErrNotExist
	}

	delete(networks.s.networks, id)
	Publish(networks.s.queue, EventDeleteNetwork{Network: n})
	return nil
}

// Get looks up a network by ID.
// Returns nil if the network doesn't exist.
func (networks networks) Get(id string) *api.Network {
	return networks.s.networks[id]
}

// Find selects a set of networks and returns them. If by is nil,
// returns all networks.
func (networks networks) Find(by By) ([]*api.Network, error) {
	switch v := by.(type) {
	case all:
		return networks.s.listNetworks(), nil
	case byName:
		return networks.s.networksByName(string(v)), nil
	default:
		return nil, ErrInvalidFindBy
	}
}

// listNetworks returns all networks that are present in the store.
func (s *MemoryStore) listNetworks() []*api.Network {
	networks := []*api.Network{}
	for _, n := range s.networks {
		networks = append(networks, n)
	}
	return networks
}

// networksByName returns the list of networks matching a given name.
// Names are neither required nor guaranteed to be unique therefore networksByName
// might return more than one network for a given name or no networks at all.
func (s *MemoryStore) networksByName(name string) []*api.Network {
	//TODO(aluzzardi): This needs an index.
	networks := []*api.Network{}
	for _, n := range s.networks {
		if n.Spec.Meta.Name == name {
			networks = append(networks, n)
		}
	}
	return networks
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
		s.nodes[n.ID] = n
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
