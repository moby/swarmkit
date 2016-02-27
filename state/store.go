package state

import (
	"errors"

	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/watch"
)

var (
	// ErrExist is returned by create operations if the provided ID is already
	// taken.
	ErrExist = errors.New("object already exists")

	// ErrNotExist is returned by altering operations (update, delete) if the
	// provided ID is not found.
	ErrNotExist = errors.New("object does not exist")

	// ErrInvalidFindBy is returned if an unrecognized type is passed to Find.
	ErrInvalidFindBy = errors.New("invalid find argument type")
)

// NodeSetWriter is the write half of a node dataset.
type NodeSetWriter interface {
	Create(n *api.Node) error
	Update(n *api.Node) error
	Delete(id string) error
}

// NodeSetReader is the read half of a node dataset.
type NodeSetReader interface {
	// Get returns the node with this ID, or nil if none exists with the
	// specified ID.
	Get(id string) *api.Node
	// Find selects a set of nodes and returns them. If by is nil,
	// returns all nodes.
	Find(by By) ([]*api.Node, error)
}

// NodeSet is a readable and writable consistent view of nodes.
type NodeSet interface {
	NodeSetReader
	NodeSetWriter
}

// JobSetWriter is the write half of a job dataset.
type JobSetWriter interface {
	Create(j *api.Job) error
	Update(j *api.Job) error
	Delete(id string) error
}

// JobSetReader is the read half of a job dataset.
type JobSetReader interface {
	// Get returns the job with this ID, or nil if none exists with the
	// specified ID.
	Get(id string) *api.Job
	// Find selects a set of jobs and returns them. If by is nil,
	// returns all jobs.
	Find(by By) ([]*api.Job, error)
}

// JobSet is a readable and writable consistent view of jobs.
type JobSet interface {
	JobSetReader
	JobSetWriter
}

// TaskSetWriter is the write half of a task dataset.
type TaskSetWriter interface {
	Create(t *api.Task) error
	Update(t *api.Task) error
	Delete(id string) error
}

// TaskSetReader is the read half of a task dataset.
type TaskSetReader interface {
	// Get returns the task with this ID, or nil if none exists with the
	// specified ID.
	Get(id string) *api.Task
	// Find selects a set of tasks and returns them. If by is nil,
	// returns all tasks.
	Find(by By) ([]*api.Task, error)
}

// TaskSet is a readable and writable consistent view of tasks.
type TaskSet interface {
	TaskSetReader
	TaskSetWriter
}

// ReadTx is a read transaction. Note that transaction does not imply
// any internal batching. It only means that the transaction presents a
// consistent view of the data that cannot be affected by other
// transactions.
type ReadTx interface {
	Nodes() NodeSetReader
	Jobs() JobSetReader
	Tasks() TaskSetReader
	Close() error
}

// Tx is a read/write transaction. Note that transaction does not imply
// any internal batching. The purpose of this transaction is to give the
// user a guarantee that its changes won't be visible to other transactions
// until the transaction is over.
type Tx interface {
	Nodes() NodeSet
	Jobs() JobSet
	Tasks() TaskSet
	Close() error
}

// A StoreCopier is capable of reading the full contents of a store from a
// read transaction, to take a snapshot.
type StoreCopier interface {
	// Copy reads a full snapshot from the provided read transaction.
	CopyFrom(tx ReadTx) error
}

// Store provides primitives for storing, accessing and manipulating swarm
// objects.
type Store interface {
	StoreCopier

	// Begin starts a full transaction that allows reads and writes. The
	// transaction must be committed with Close.
	Begin() (Tx, error)

	// BeginRead starts a transaction that reads only. The transaction must
	// be finalized with Close.
	BeginRead() (ReadTx, error)
}

// WatchableStore is an extension of Store that publishes modifications to a
// watch queue.
type WatchableStore interface {
	Store

	// WatchQueue returns the publish/subscribe queue where watchers can
	// be registered. This is exposed directly to avoid forcing every store
	// implementation to provide a full set of conveninence functions.
	WatchQueue() *watch.Queue

	// Snapshot copies the state of this Store. It also returns a channel
	// that will return further events from this point so the snapshot can be
	// kept up to date. The watch channel must be released with
	// watch.StopWatch when it is no longer needed.
	Snapshot(storeCopier StoreCopier) (chan watch.Event, error)
}

// By is an interface type passed to Find methods. Implementations must be
// defined in this package.
type By interface {
	// isBy allows this interface to only be satisfied by certain internal
	// types.
	isBy()
}

type all struct{}

func (a all) isBy() {
}

// All is an argument that can be passed to find to list all items in the
// set.
var All all

type byName string

func (b byName) isBy() {
}

// ByName creates an object to pass to Find to select by name.
func ByName(name string) By {
	return byName(name)
}

type byJob string

func (b byJob) isBy() {
}

// ByJobID creates an object to pass to Find to select by job.
func ByJobID(jobID string) By {
	return byJob(jobID)
}

type byNode string

func (b byNode) isBy() {
}

// ByNodeID creates an object to pass to Find to select by node.
func ByNodeID(nodeID string) By {
	return byNode(nodeID)
}
