package store

import (
	"errors"
	"fmt"
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

	tx := tx{
		memDBTx:    memDBTx,
		curVersion: curVersion,
	}
	tx.nodes = nodes{
		tx:      &tx,
		memDBTx: memDBTx,
	}
	tx.services = services{
		tx:      &tx,
		memDBTx: memDBTx,
	}
	tx.tasks = tasks{
		tx:      &tx,
		memDBTx: memDBTx,
	}
	tx.networks = networks{
		tx:      &tx,
		memDBTx: memDBTx,
	}
	tx.volumes = volumes{
		tx:      &tx,
		memDBTx: memDBTx,
	}

	err := cb(tx)

	if err == nil {
		if proposer == nil {
			memDBTx.Commit()
		} else {
			var sa []*api.StoreAction
			sa, err = tx.newStoreAction()

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

func (tx tx) newStoreAction() ([]*api.StoreAction, error) {
	var actions []*api.StoreAction

changelist:
	for _, c := range tx.changelist {
		for _, os := range objectStorers {
			sa, err := os.NewStoreAction(c)
			if err == nil {
				actions = append(actions, &sa)
				continue changelist
			} else if err != errUnknownStoreAction {
				return nil, err
			}
		}

		return nil, errors.New("unrecognized event type")
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
