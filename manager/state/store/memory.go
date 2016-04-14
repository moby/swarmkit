package store

import (
	"errors"
	"fmt"
	"runtime"
	"sync"

	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/manager/state"
	"github.com/docker/swarm-v2/manager/state/pb"
	"github.com/docker/swarm-v2/manager/state/watch"
	memdb "github.com/hashicorp/go-memdb"
	"golang.org/x/net/context"
)

const (
	indexID        = "id"
	indexName      = "name"
	indexServiceID = "serviceid"
	indexNodeID    = "nodeid"

	prefix = "_prefix"

	// MaxChangesPerTransaction is the number of changes after which a new
	// transaction should be started within Batch.
	MaxChangesPerTransaction = 200
)

var (
	objectStorers []ObjectStoreConfig
	schema        = &memdb.DBSchema{
		Tables: map[string]*memdb.TableSchema{},
	}
	errUnknownStoreAction = errors.New("unknown store action")
)

func register(os ObjectStoreConfig) {
	objectStorers = append(objectStorers, os)
	schema.Tables[os.Name] = os.Table
}

// MemoryStore is a concurrency-safe, in-memory implementation of the Store
// interface.
type MemoryStore struct {
	// updateLock must be held during an update transaction.
	updateLock sync.Mutex

	memDB *memdb.MemDB
	queue *watch.Queue

	proposer state.Proposer
}

// NewMemoryStore returns an in-memory store. The argument is a optional
// Proposer which will be used to propagate changes to other members in a
// cluster.
func NewMemoryStore(proposer state.Proposer) *MemoryStore {
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
	services services
	tasks    tasks
	networks networks
	volumes  volumes
}

// View executes a read transaction.
func (s *MemoryStore) View(cb func(state.ReadTx) error) error {
	memDBTx := s.memDB.Txn(false)

	readTx := readTx{
		nodes: nodes{
			memDBTx: memDBTx,
		},
		services: services{
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

func (t readTx) Nodes() state.NodeSetReader {
	return t.nodes
}

func (t readTx) Services() state.ServiceSetReader {
	return t.services
}

func (t readTx) Networks() state.NetworkSetReader {
	return t.networks
}

func (t readTx) Tasks() state.TaskSetReader {
	return t.tasks
}

func (t readTx) Volumes() state.VolumeSetReader {
	return t.volumes
}

type tx struct {
	nodes      nodes
	services   services
	tasks      tasks
	networks   networks
	volumes    volumes
	curVersion *api.Version
	memDBTx    *memdb.Txn
	changelist []state.Event
}

// ApplyStoreActions updates a store based on StoreAction messages.
func (s *MemoryStore) ApplyStoreActions(actions []*api.StoreAction) error {
	s.updateLock.Lock()
	memDBTx := s.memDB.Txn(true)

	tx := tx{
		nodes:    nodes{memDBTx: memDBTx},
		services: services{memDBTx: memDBTx},
		tasks:    tasks{memDBTx: memDBTx},
		networks: networks{memDBTx: memDBTx},
		volumes:  volumes{memDBTx: memDBTx},
		memDBTx:  memDBTx,
	}
	tx.nodes.tx = &tx
	tx.services.tx = &tx
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
		s.queue.Publish(state.EventCommit{})
	}
	s.updateLock.Unlock()
	return nil
}

func applyStoreAction(tx tx, sa *api.StoreAction) error {
	for _, os := range objectStorers {
		err := os.ApplyStoreAction(tx, sa)
		if err != errUnknownStoreAction {
			return err
		}
	}

	return errors.New("unrecognized action type")
}

func (s *MemoryStore) update(proposer state.Proposer, cb func(state.Tx) error) error {
	s.updateLock.Lock()
	memDBTx := s.memDB.Txn(true)

	var curVersion *api.Version

	if proposer != nil {
		curVersion = proposer.GetVersion()
	}

	var tx tx
	tx.init(memDBTx, curVersion)

	err := cb(tx)

	if err == nil {
		if proposer == nil {
			memDBTx.Commit()
		} else {
			var sa []*api.StoreAction
			sa, err = tx.changelistStoreActions()

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
			s.queue.Publish(state.EventCommit{})
		}
	} else {
		memDBTx.Abort()
	}
	s.updateLock.Unlock()
	return err

}

func (s *MemoryStore) updateLocal(cb func(state.Tx) error) error {
	return s.update(nil, cb)
}

// Update executes a read/write transaction.
func (s *MemoryStore) Update(cb func(state.Tx) error) error {
	return s.update(s.proposer, cb)
}

type batch struct {
	tx                      tx
	store                   *MemoryStore
	applied                 int
	committed               int
	transactionSizeEstimate int
	changelistLen           int
	err                     error
}

func (batch *batch) Update(cb func(state.Tx) error) error {
	if batch.err != nil {
		return batch.err
	}

	if err := cb(batch.tx); err != nil {
		return err
	}

	batch.applied++

	// TODO(aaronl): Creating a StoreAction and marshalling it here
	// for the size estimate is redundant because it gets created
	// and marshalled a second time at commit time. This could be
	// avoided by doing the marshalling only here, and streaming
	// the serialized StoreActions to the proposer, which it would
	// write using gogo-protobuf's io.DelimitedWriter. This would
	// take some interface changes, so I'm leaving it as a TODO for
	// now.
	for batch.changelistLen < len(batch.tx.changelist) {
		sa, err := newStoreAction(batch.tx.changelist[batch.changelistLen])
		if err != nil {
			return err
		}
		marshalled, err := sa.Marshal()
		if err != nil {
			return err
		}
		batch.transactionSizeEstimate += len(marshalled)
		batch.changelistLen++
	}

	if batch.changelistLen >= MaxChangesPerTransaction || batch.transactionSizeEstimate >= (state.MaxTransactionBytes*3)/4 {
		if err := batch.commit(); err != nil {
			return err
		}

		// Yield the update lock
		batch.store.updateLock.Unlock()
		runtime.Gosched()
		batch.store.updateLock.Lock()

		batch.newTx()
	}

	return nil
}

func (batch *batch) newTx() {
	var curVersion *api.Version

	if batch.store.proposer != nil {
		curVersion = batch.store.proposer.GetVersion()
	}

	batch.tx.init(batch.store.memDB.Txn(true), curVersion)
	batch.transactionSizeEstimate = 0
	batch.changelistLen = 0
}

func (batch *batch) commit() error {
	if batch.store.proposer != nil {
		var sa []*api.StoreAction
		sa, batch.err = batch.tx.changelistStoreActions()

		if batch.err == nil {
			if sa != nil {
				batch.err = batch.store.proposer.ProposeValue(context.Background(), sa, func() {
					batch.tx.memDBTx.Commit()
				})
			} else {
				batch.tx.memDBTx.Commit()
			}
		}
	} else {
		batch.tx.memDBTx.Commit()
	}

	if batch.err != nil {
		batch.tx.memDBTx.Abort()
		return batch.err
	}

	batch.committed = batch.applied

	for _, c := range batch.tx.changelist {
		batch.store.queue.Publish(c)
	}
	if len(batch.tx.changelist) != 0 {
		batch.store.queue.Publish(state.EventCommit{})
	}

	return nil
}

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
func (s *MemoryStore) Batch(cb func(state.Batch) error) (int, error) {
	s.updateLock.Lock()

	batch := batch{
		store: s,
	}
	batch.newTx()

	if err := cb(&batch); err != nil {
		batch.tx.memDBTx.Abort()
		s.updateLock.Unlock()
		return batch.committed, err
	}

	err := batch.commit()
	s.updateLock.Unlock()
	return batch.committed, err
}

func (tx *tx) init(memDBTx *memdb.Txn, curVersion *api.Version) {
	tx.memDBTx = memDBTx
	tx.curVersion = curVersion
	tx.changelist = nil

	tx.nodes = nodes{
		tx:      tx,
		memDBTx: memDBTx,
	}
	tx.services = services{
		tx:      tx,
		memDBTx: memDBTx,
	}
	tx.tasks = tasks{
		tx:      tx,
		memDBTx: memDBTx,
	}
	tx.networks = networks{
		tx:      tx,
		memDBTx: memDBTx,
	}
	tx.volumes = volumes{
		tx:      tx,
		memDBTx: memDBTx,
	}
}

func newStoreAction(c state.Event) (*api.StoreAction, error) {
	for _, os := range objectStorers {
		sa, err := os.NewStoreAction(c)
		if err == nil {
			return &sa, nil
		} else if err != errUnknownStoreAction {
			return nil, err
		}
	}

	return nil, errors.New("unrecognized event type")
}

func (tx tx) changelistStoreActions() ([]*api.StoreAction, error) {
	var actions []*api.StoreAction

	for _, c := range tx.changelist {
		sa, err := newStoreAction(c)
		if err != nil {
			return nil, err
		}
		actions = append(actions, sa)
	}

	return actions, nil
}

func (tx tx) Nodes() state.NodeSet {
	return tx.nodes
}

func (tx tx) Networks() state.NetworkSet {
	return tx.networks
}

func (tx tx) Services() state.ServiceSet {
	return tx.services
}

func (tx tx) Tasks() state.TaskSet {
	return tx.tasks
}

func (tx tx) Volumes() state.VolumeSet {
	return tx.volumes
}

// lookup is an internal typed wrapper around memdb.
func lookup(memDBTx *memdb.Txn, table, index, id string) state.Object {
	j, err := memDBTx.First(table, index, id)
	if err != nil {
		return nil
	}
	if j != nil {
		return j.(state.Object)
	}
	return nil
}

// create adds a new object to the store.
// Returns ErrExist if the ID is already taken.
func (tx *tx) create(table string, o state.Object) error {
	if lookup(tx.memDBTx, table, indexID, o.ID()) != nil {
		return state.ErrExist
	}

	copy := o.Copy(tx.curVersion)
	err := tx.memDBTx.Insert(table, copy)
	if err == nil {
		tx.changelist = append(tx.changelist, copy.EventCreate())
	}
	return err
}

// Update updates an existing object in the store.
// Returns ErrNotExist if the object doesn't exist.
func (tx *tx) update(table string, o state.Object) error {
	oldN := lookup(tx.memDBTx, table, indexID, o.ID())
	if oldN == nil {
		return state.ErrNotExist
	}

	if tx.curVersion != nil {
		if oldN.(state.Object).Version() != o.Version() {
			return state.ErrSequenceConflict
		}
	}

	copy := o.Copy(tx.curVersion)

	err := tx.memDBTx.Insert(table, copy)
	if err == nil {
		tx.changelist = append(tx.changelist, copy.EventUpdate())
	}
	return err
}

// Delete removes an object from the store.
// Returns ErrNotExist if the object doesn't exist.
func (tx *tx) delete(table, id string) error {
	n := lookup(tx.memDBTx, table, indexID, id)
	if n == nil {
		return state.ErrNotExist
	}

	err := tx.memDBTx.Delete(table, n)
	if err == nil {
		tx.changelist = append(tx.changelist, n.EventDelete())
	}
	return err
}

// Get looks up an object by ID.
// Returns nil if the object doesn't exist.
func get(memDBTx *memdb.Txn, table, id string) state.Object {
	o := lookup(memDBTx, table, indexID, id)
	if o == nil {
		return nil
	}
	return o.Copy(nil)
}

// find selects a set of objects calls a callback for each matching object.
func find(memDBTx *memdb.Txn, table string, by state.By, cb func(state.Object)) error {
	fromResultIterator := func(it memdb.ResultIterator) {
		for {
			obj := it.Next()
			if obj == nil {
				break
			}
			cb(obj.(state.Object).Copy(nil))
		}
	}

	fromResultIterators := func(its ...memdb.ResultIterator) {
		ids := make(map[string]struct{})
		for _, it := range its {
			for {
				obj := it.Next()
				if obj == nil {
					break
				}
				o := obj.(state.Object)
				id := o.ID()
				if _, exists := ids[id]; !exists {
					cb(o.Copy(nil))
					ids[id] = struct{}{}
				}
			}
		}
	}

	switch v := by.(type) {
	case state.AllFinder:
		it, err := memDBTx.Get(table, indexID)
		if err != nil {
			return err
		}
		fromResultIterator(it)
	case state.NameFinder:
		it, err := memDBTx.Get(table, indexName, string(v))
		if err != nil {
			return err
		}
		fromResultIterator(it)
	case state.QueryFinder:
		itID, err := memDBTx.Get(table, indexID+prefix, string(v))
		if err != nil {
			return err
		}
		itName, err := memDBTx.Get(table, indexName, string(v))
		if err != nil {
			return err
		}
		fromResultIterators(itID, itName)
	case state.NodeFinder:
		it, err := memDBTx.Get(table, indexNodeID, string(v))
		if err != nil {
			return err
		}
		fromResultIterator(it)
	case state.ServiceFinder:
		it, err := memDBTx.Get(table, indexServiceID, string(v))
		if err != nil {
			return err
		}
		fromResultIterator(it)
	default:
		return state.ErrInvalidFindBy
	}
	return nil
}

// Save serializes the data in the store.
func (s *MemoryStore) Save(tx state.ReadTx) (*pb.StoreSnapshot, error) {
	var snapshot pb.StoreSnapshot
	for _, os := range objectStorers {
		if err := os.Save(tx, &snapshot); err != nil {
			return nil, err
		}
	}

	return &snapshot, nil
}

// Restore sets the contents of the store to the serialized data in the
// argument.
func (s *MemoryStore) Restore(snapshot *pb.StoreSnapshot) error {
	return s.updateLocal(func(tx state.Tx) error {
		for _, os := range objectStorers {
			if err := os.Restore(tx, snapshot); err != nil {
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
