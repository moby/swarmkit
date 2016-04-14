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

	// Update performs a full transaction that allows reads and writes.
	// Within the callback function, changes can safely be made through the
	// Tx interface. If the callback function returns nil, Update will
	// attempt to commit the transaction.
	Update(func(Tx) error) error

	// View performs a transaction that only includes reads. Within the
	// callback function, a consistent view of the data is available through
	// the ReadTx interface.
	View(func(ReadTx) error) error

	// Save serializes the data in the store.
	Save(ReadTx) (*pb.StoreSnapshot, error)

	// Restore sets the contents of the store to the serialized data in the
	// argument.
	Restore(*pb.StoreSnapshot) error
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

// DeleteAll clears the contents of a store.
func DeleteAll(tx Tx) error {
	nodes, err := tx.Nodes().Find(All)
	if err != nil {
		return err
	}
	for _, n := range nodes {
		if err := tx.Nodes().Delete(n.ID); err != nil {
			return err
		}
	}

	services, err := tx.Services().Find(All)
	if err != nil {
		return err
	}
	for _, j := range services {
		if err := tx.Services().Delete(j.ID); err != nil {
			return err
		}
	}

	networks, err := tx.Networks().Find(All)
	if err != nil {
		return err
	}
	for _, n := range networks {
		if err := tx.Networks().Delete(n.ID); err != nil {
			return err
		}
	}

	tasks, err := tx.Tasks().Find(All)
	if err != nil {
		return err
	}
	for _, t := range tasks {
		if err := tx.Tasks().Delete(t.ID); err != nil {
			return err
		}
	}

	volumes, err := tx.Volumes().Find(All)
	if err != nil {
		return err
	}
	for _, v := range volumes {
		if err := tx.Volumes().Delete(v.ID); err != nil {
			return err
		}
	}

	return nil
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

type byService string

func (b byService) isBy() {
}

// ByServiceID creates an object to pass to Find to select by service.
func ByServiceID(serviceID string) By {
	return byService(serviceID)
}

type byNode string

func (b byNode) isBy() {
}

// ByNodeID creates an object to pass to Find to select by node.
func ByNodeID(nodeID string) By {
	return byNode(nodeID)
}

type byQuery string

func (b byQuery) isBy() {
}

// ByQuery creates an object to pass to Find to select by query.
func ByQuery(query string) By {
	return byQuery(query)
}
