package state

import (
	"errors"

	"github.com/docker/swarm-v2/api"
)

var (
	// ErrExist is returned by create operations if the provided ID is already
	// taken.
	ErrExist = errors.New("object already exists")

	// ErrNotExist is returned by altering operations (update, delete) if the
	// provided ID is not found.
	ErrNotExist = errors.New("object does not exist")
)

// Store provides primitives for storing, accessing and manipulating swarm objects.
type Store interface {
	// CreateNode adds a new node to the store.
	// Returns ErrExist if the ID is already taken.
	CreateNode(id string, n *api.Node) error

	// UpdateNode updates an existing node in the store.
	// Returns ErrNotExist if the node doesn't exist.
	UpdateNode(id string, n *api.Node) error

	// DeleteNode removes a node from the store.
	// Returns ErrNotExist if the node doesn't exist.
	DeleteNode(id string) error

	// Nodes returns all nodes that are present in the store.
	Nodes() []*api.Node

	// Node looks up a node by ID.
	// Returns nil if the node doesn't exist.
	Node(id string) *api.Node

	// NodesByName returns the list of nodes matching a given name.
	// Names are neither required nor guaranteed to be unique therefore NodesByName
	// might return more than one node for a given name or no nodes at all.
	NodesByName(name string) []*api.Node

	// CreateTask adds a new task to the store.
	// Returns ErrExist if the ID is already taken.
	CreateTask(id string, t *api.Task) error

	// UpdateTask updates an existing task in the store.
	// Returns ErrNotExist if the task doesn't exist.
	UpdateTask(id string, t *api.Task) error

	// DeleteTask removes a task from the store.
	// Returns ErrNotExist if the task doesn't exist.
	DeleteTask(id string) error

	// Tasks returns all tasks that are present in the store.
	Tasks() []*api.Task

	// Task looks up a task by ID.
	// Returns nil if the task doesn't exist.
	Task(id string) *api.Task

	// TasksByName returns the list of tasks matching a given name.
	// Names are neither required nor guaranteed to be unique therefore TasksByName
	// might return more than one task for a given name or no tasks at all.
	TasksByName(name string) []*api.Task

	// TasksByJob returns the list of tasks belonging to a particular Job.
	TasksByJob(jobID string) []*api.Task

	// TasksByNode returns the list of tasks assigned to a particular Node.
	TasksByNode(nodeID string) []*api.Task

	// CreateJob adds a new job to the store.
	// Returns ErrExist if the ID is already taken.
	CreateJob(id string, j *api.Job) error

	// UpdateJob updates an existing job in the store.
	// Returns ErrNotExist if the job doesn't exist.
	UpdateJob(id string, j *api.Job) error

	// DeleteJob removes a job from the store.
	// Returns ErrNotExist if the node doesn't exist.
	DeleteJob(id string) error

	// Jobs returns all jobs that are present in the store.
	Jobs() []*api.Job

	// Job looks up a job by ID.
	// Returns nil if the job doesn't exist.
	Job(id string) *api.Job

	// JobsByName returns the list of jobs matching a given name.
	// Names are neither required nor guaranteed to be unique therefore JobsByName
	// might return more than one node for a given name or no nodes at all.
	JobsByName(name string) []*api.Job
}
