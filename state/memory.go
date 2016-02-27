package state

import (
	"sync"

	"github.com/docker/swarm-v2/api"
)

// MemoryStore is a concurrency-safe, in-memory implementation of the Store
// interface.
type MemoryStore struct {
	l sync.RWMutex

	nodes map[string]*api.Node
	tasks map[string]*api.Task
	jobs  map[string]*api.Job
}

// NewMemoryStore returns an in-memory store.
func NewMemoryStore() Store {
	return &MemoryStore{
		nodes: make(map[string]*api.Node),
		tasks: make(map[string]*api.Task),
		jobs:  make(map[string]*api.Job),
	}
}

// CreateNode adds a new node to the store.
// Returns ErrExist if the ID is already taken.
func (s *MemoryStore) CreateNode(id string, n *api.Node) error {
	s.l.Lock()
	defer s.l.Unlock()

	if _, ok := s.nodes[id]; ok {
		return ErrExist
	}

	s.nodes[id] = n
	return nil
}

// UpdateNode updates an existing node in the store.
// Returns ErrNotExist if the node doesn't exist.
func (s *MemoryStore) UpdateNode(id string, n *api.Node) error {
	s.l.Lock()
	defer s.l.Unlock()

	if _, ok := s.nodes[id]; !ok {
		return ErrNotExist
	}

	s.nodes[id] = n
	return nil
}

// DeleteNode removes a node from the store.
// Returns ErrNotExist if the node doesn't exist.
func (s *MemoryStore) DeleteNode(id string) error {
	s.l.Lock()
	defer s.l.Unlock()

	if _, ok := s.nodes[id]; !ok {
		return ErrNotExist
	}

	delete(s.nodes, id)
	return nil
}

// Nodes returns all nodes that are present in the store.
func (s *MemoryStore) Nodes() []*api.Node {
	s.l.RLock()
	defer s.l.RUnlock()

	nodes := []*api.Node{}
	for _, n := range s.nodes {
		nodes = append(nodes, n)
	}

	return nodes
}

// Node looks up a node by ID.
// Returns nil if the node doesn't exist.
func (s *MemoryStore) Node(id string) *api.Node {
	s.l.RLock()
	defer s.l.RUnlock()

	return s.nodes[id]
}

// NodesByName returns the list of nodes matching a given name.
// Names are neither required nor guaranteed to be unique therefore NodesByName
// might return more than one node for a given name or no nodes at all.
func (s *MemoryStore) NodesByName(name string) []*api.Node {
	s.l.RLock()
	defer s.l.RUnlock()

	//TODO(aluzzardi): This needs an index.
	nodes := []*api.Node{}
	for _, n := range s.nodes {
		if n.Spec.Meta.Name == name {
			nodes = append(nodes, n)
		}
	}
	return nodes
}

// CreateTask adds a new task to the store.
// Returns ErrExist if the ID is already taken.
func (s *MemoryStore) CreateTask(id string, t *api.Task) error {
	s.l.Lock()
	defer s.l.Unlock()

	if _, ok := s.tasks[id]; ok {
		return ErrExist
	}

	s.tasks[id] = t
	return nil
}

// UpdateTask updates an existing task in the store.
// Returns ErrNotExist if the task doesn't exist.
func (s *MemoryStore) UpdateTask(id string, t *api.Task) error {
	s.l.Lock()
	defer s.l.Unlock()

	if _, ok := s.tasks[id]; !ok {
		return ErrNotExist
	}

	s.tasks[id] = t
	return nil
}

// DeleteTask removes a task from the store.
// Returns ErrNotExist if the task doesn't exist.
func (s *MemoryStore) DeleteTask(id string) error {
	s.l.Lock()
	defer s.l.Unlock()

	if _, ok := s.tasks[id]; !ok {
		return ErrNotExist
	}

	delete(s.tasks, id)
	return nil
}

// Tasks returns all tasks that are present in the store.
func (s *MemoryStore) Tasks() []*api.Task {
	s.l.RLock()
	defer s.l.RUnlock()

	tasks := []*api.Task{}
	for _, t := range s.tasks {
		tasks = append(tasks, t)
	}
	return tasks
}

// Task looks up a task by ID.
// Returns nil if the task doesn't exist.
func (s *MemoryStore) Task(id string) *api.Task {
	s.l.RLock()
	defer s.l.RUnlock()

	return s.tasks[id]
}

// TasksByName returns the list of tasks matching a given name.
// Names are neither required nor guaranteed to be unique therefore TasksByName
// might return more than one task for a given name or no tasks at all.
func (s *MemoryStore) TasksByName(name string) []*api.Task {
	s.l.RLock()
	defer s.l.RUnlock()

	//TODO(aluzzardi): This needs an index.
	tasks := []*api.Task{}
	for _, t := range s.tasks {
		if t.Spec.Meta.Name == name {
			tasks = append(tasks, t)
		}
	}
	return tasks
}

// TasksByJob returns the list of tasks belonging to a particular Job.
func (s *MemoryStore) TasksByJob(jobID string) []*api.Task {
	s.l.RLock()
	defer s.l.RUnlock()

	//TODO(aluzzardi): This needs an index.
	tasks := []*api.Task{}
	for _, t := range s.tasks {
		if t.JobID == jobID {
			tasks = append(tasks, t)
		}
	}
	return tasks
}

// TasksByNode returns the list of tasks assigned to a particular Node.
func (s *MemoryStore) TasksByNode(nodeID string) []*api.Task {
	s.l.RLock()
	defer s.l.RUnlock()

	//TODO(aluzzardi): This needs an index.
	tasks := []*api.Task{}
	for _, t := range s.tasks {
		if t.NodeID == nodeID {
			tasks = append(tasks, t)
		}
	}
	return tasks
}

// CreateJob adds a new job to the store.
// Returns ErrExist if the ID is already taken.
func (s *MemoryStore) CreateJob(id string, j *api.Job) error {
	s.l.Lock()
	defer s.l.Unlock()

	if _, ok := s.jobs[id]; ok {
		return ErrExist
	}

	s.jobs[id] = j
	return nil
}

// UpdateJob updates an existing job in the store.
// Returns ErrNotExist if the job doesn't exist.
func (s *MemoryStore) UpdateJob(id string, j *api.Job) error {
	s.l.Lock()
	defer s.l.Unlock()

	if _, ok := s.jobs[id]; !ok {
		return ErrNotExist
	}

	s.jobs[id] = j
	return nil
}

// DeleteJob removes a job from the store.
// Returns ErrNotExist if the node doesn't exist.
func (s *MemoryStore) DeleteJob(id string) error {
	s.l.Lock()
	defer s.l.Unlock()

	if _, ok := s.jobs[id]; !ok {
		return ErrNotExist
	}

	delete(s.jobs, id)
	return nil
}

// Jobs returns all jobs that are present in the store.
func (s *MemoryStore) Jobs() []*api.Job {
	s.l.RLock()
	defer s.l.RUnlock()

	jobs := []*api.Job{}
	for _, j := range s.jobs {
		jobs = append(jobs, j)
	}
	return jobs
}

// Job looks up a job by ID.
// Returns nil if the job doesn't exist.
func (s *MemoryStore) Job(id string) *api.Job {
	s.l.RLock()
	defer s.l.RUnlock()

	return s.jobs[id]
}

// JobsByName returns the list of jobs matching a given name.
// Names are neither required nor guaranteed to be unique therefore JobsByName
// might return more than one node for a given name or no nodes at all.
func (s *MemoryStore) JobsByName(name string) []*api.Job {
	s.l.RLock()
	defer s.l.RUnlock()

	//TODO(aluzzardi): This needs an index.
	jobs := []*api.Job{}
	for _, j := range s.jobs {
		if j.Spec.Meta.Name == name {
			jobs = append(jobs, j)
		}
	}
	return jobs
}
