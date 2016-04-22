package state

import (
	"errors"

	"github.com/docker/go-events"
	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/manager/state/pb"
	"github.com/docker/swarm-v2/manager/state/watch"
)

var (
	// ErrExist is returned by create operations if the provided ID is already
	// taken.
	ErrExist = errors.New("object already exists")

	// ErrNotExist is returned by altering operations (update, delete) if the
	// provided ID is not found.
	ErrNotExist = errors.New("object does not exist")

	// ErrNameConflict is returned by create/update if the object name is
	// already in use by another object.
	ErrNameConflict = errors.New("name conflicts with an existing object")

	// ErrInvalidFindBy is returned if an unrecognized type is passed to Find.
	ErrInvalidFindBy = errors.New("invalid find argument type")

	// ErrSequenceConflict is returned when trying to update an object
	// whose sequence information does not match the object in the store's.
	ErrSequenceConflict = errors.New("update out of sequence")
)

const (
	// MaxTransactionBytes is the maximum serialized transaction size.
	MaxTransactionBytes = 1.5 * 1024 * 1024
)

// Object is a generic object that can be handled by the store.
type Object interface {
	ID() string               // Get ID
	Version() api.Version     // Retrieve version information
	Copy(*api.Version) Object // Return a copy of this object, optionally setting new version information on the copy
	EventCreate() Event       // Return a creation event
	EventUpdate() Event       // Return an update event
	EventDelete() Event       // Return a deletion event
}

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

// ServiceSetWriter is the write half of a service dataset.
type ServiceSetWriter interface {
	Create(j *api.Service) error
	Update(j *api.Service) error
	Delete(id string) error
}

// ServiceSetReader is the read half of a service dataset.
type ServiceSetReader interface {
	// Get returns the service with this ID, or nil if none exists with the
	// specified ID.
	Get(id string) *api.Service
	// Find selects a set of services and returns them. If by is nil,
	// returns all services.
	Find(by By) ([]*api.Service, error)
}

// ServiceSet is a readable and writable consistent view of services.
type ServiceSet interface {
	ServiceSetReader
	ServiceSetWriter
}

// NetworkSetWriter is the write half of a network dataset.
type NetworkSetWriter interface {
	Create(n *api.Network) error
	Update(n *api.Network) error
	Delete(id string) error
}

// NetworkSetReader is the read half of a network dataset.
type NetworkSetReader interface {
	// Get returns the network with this ID, or nil if none exists with the
	// specified ID.
	Get(id string) *api.Network
	// Find selects a set of networks and returns them. If by is nil,
	// returns all services.
	Find(by By) ([]*api.Network, error)
}

// NetworkSet is a readable and writable consistent view of networks.
type NetworkSet interface {
	NetworkSetReader
	NetworkSetWriter
}

// VolumeSetWriter is the write half of a volume dataset.
type VolumeSetWriter interface {
	Create(v *api.Volume) error
	Update(v *api.Volume) error
	Delete(id string) error
}

// VolumeSetReader is the read half of a volume dataset.
type VolumeSetReader interface {
	// Get returns the volume with this ID, or nil if none exists with the
	// specified ID.
	Get(id string) *api.Volume
	// Find selects a set of volumes and returns them. If by is nil,
	// returns all services.
	Find(by By) ([]*api.Volume, error)
}

// VolumeSet is a readable and writable consistent view of volumes.
type VolumeSet interface {
	VolumeSetReader
	VolumeSetWriter
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
	Services() ServiceSetReader
	Networks() NetworkSetReader
	Tasks() TaskSetReader
	Volumes() VolumeSetReader
}

// Tx is a read/write transaction. Note that transaction does not imply
// any internal batching. The purpose of this transaction is to give the
// user a guarantee that its changes won't be visible to other transactions
// until the transaction is over.
type Tx interface {
	Nodes() NodeSet
	Services() ServiceSet
	Networks() NetworkSet
	Tasks() TaskSet
	Volumes() VolumeSet
}

// Batch provides an interface to batch updates to a store. Each call to
// Update is atomic, but different calls to Update may be spread across
// multiple transactions to circumvent transaction size limits.
type Batch interface {
	Update(func(Tx) error) error
}

// Store provides primitives for storing, accessing and manipulating swarm
// objects.
// TODO(aaronl): Refactor this interface to work on generic Objects. Provide
// type-specific helpers to manipulate objects in a type-safe way.
type Store interface {
	// Update performs a full transaction that allows reads and writes.
	// Within the callback function, changes can safely be made through the
	// Tx interface. If the callback function returns nil, Update will
	// attempt to commit the transaction.
	Update(func(Tx) error) error

	// Batch performs one or more transactions that allow reads and writes
	// It invokes a callback that is passed a Batch object. The callback may
	// call batch.Update for each change it wants to make as part of the
	// batch. The changes in the batch may be split over multiple
	// transactions if necessary to keep transactions below the size limit.
	// Batch holds a lock over the state, but will yield this lock every
	// it creates a new transaction to allow other writers to proceed.
	// Thus, unrelated changes to the state may occur between calls to
	// batch.Update.
	//
	// This method allows the caller to iterate over a data set and apply
	// changes in sequence without holding the store write lock for an
	// excessive time, or producing a transaction that exceeds the maximum
	// size.
	//
	// Batch returns the number of calls to batch.Update whose changes were
	// successfully committed to the store.
	Batch(cb func(batch Batch) error) (int, error)

	// View performs a transaction that only includes reads. Within the
	// callback function, a consistent view of the data is available through
	// the ReadTx interface.
	View(func(ReadTx) error) error

	// Save serializes the data in the store.
	Save(ReadTx) (*pb.StoreSnapshot, error)

	// Restore sets the contents of the store to the serialized data in the
	// argument.
	Restore(*pb.StoreSnapshot) error

	// ApplyStoreActions updates a store based on StoreAction messages.
	ApplyStoreActions(actions []*api.StoreAction) error
}

// WatchableStore is an extension of Store that publishes modifications to a
// watch queue.
type WatchableStore interface {
	Store

	// WatchQueue returns the publish/subscribe queue where watchers can
	// be registered. This is exposed directly to avoid forcing every store
	// implementation to provide a full set of conveninence functions.
	WatchQueue() *watch.Queue
}

type snapshotReadTx struct {
	tx Tx
}

func (tx snapshotReadTx) Nodes() NodeSetReader {
	return tx.tx.Nodes()
}

func (tx snapshotReadTx) Services() ServiceSetReader {
	return tx.tx.Services()
}

func (tx snapshotReadTx) Networks() NetworkSetReader {
	return tx.tx.Networks()
}

func (tx snapshotReadTx) Tasks() TaskSetReader {
	return tx.tx.Tasks()
}

func (tx snapshotReadTx) Volumes() VolumeSetReader {
	return tx.tx.Volumes()
}

// ViewAndWatch calls a callback which can observe the state of this Store. It
// also returns a channel that will return further events from this point so
// the snapshot can be kept up to date. The watch channel must be released with
// watch.StopWatch when it is no longer needed. The channel is guaranteed to
// get all events after the moment of the snapshot, and only those events.
func ViewAndWatch(store WatchableStore, cb func(ReadTx) error) (watch chan events.Event, cancel func(), err error) {
	// Using Update to lock the store and guarantee consistency between
	// the watcher and the the state seen by the callback. snapshotReadTx
	// exposes this Tx as a ReadTx so the callback can't modify it.
	err = store.Update(func(tx Tx) error {
		if err = cb(snapshotReadTx{tx: tx}); err != nil {
			return err
		}
		watch, cancel = store.WatchQueue().Watch()
		return nil
	})
	if watch != nil && err != nil {
		cancel()
		cancel = nil
		watch = nil
	}
	return
}

// By is an interface type passed to Find methods. Implementations must be
// defined in this package.
type By interface {
	// isBy allows this interface to only be satisfied by certain internal
	// types.
	isBy()
}

// AllFinder is the type used to list all items.
type AllFinder struct{}

func (a AllFinder) isBy() {
}

// All is an argument that can be passed to find to list all items in the
// set.
var All AllFinder

// NameFinder is the type used to find by name.
type NameFinder string

func (b NameFinder) isBy() {
}

// ByName creates an object to pass to Find to select by name.
func ByName(name string) By {
	return NameFinder(name)
}

// ServiceFinder is the type used to find by service ID.
type ServiceFinder string

func (b ServiceFinder) isBy() {
}

// ByServiceID creates an object to pass to Find to select by service.
func ByServiceID(serviceID string) By {
	return ServiceFinder(serviceID)
}

// NodeFinder is the type used to find by node ID.
type NodeFinder string

func (b NodeFinder) isBy() {
}

// ByNodeID creates an object to pass to Find to select by node.
func ByNodeID(nodeID string) By {
	return NodeFinder(nodeID)
}

// QueryFinder is the type used to find by query.
type QueryFinder string

func (b QueryFinder) isBy() {
}

// ByQuery creates an object to pass to Find to select by query.
func ByQuery(query string) By {
	return QueryFinder(query)
}

// ServiceModeFinder is the type used to find by service mode.
type ServiceModeFinder api.ServiceSpec_Mode

func (b ServiceModeFinder) isBy() {
}

// ByServiceMode creates an object to pass to Find to select by service mode.
func ByServiceMode(mode api.ServiceSpec_Mode) By {
	return ServiceModeFinder(mode)
}
