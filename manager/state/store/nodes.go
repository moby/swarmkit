package store

import (
	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/manager/state"
	"github.com/docker/swarm-v2/manager/state/pb"
	memdb "github.com/hashicorp/go-memdb"
)

const tableNode = "node"

func init() {
	register(ObjectStoreConfig{
		Name: tableNode,
		Table: &memdb.TableSchema{
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
		Save: func(tx state.ReadTx, snapshot *pb.StoreSnapshot) error {
			var err error
			snapshot.Nodes, err = tx.Nodes().Find(state.All)
			return err
		},
		Restore: func(tx state.Tx, snapshot *pb.StoreSnapshot) error {
			nodes, err := tx.Nodes().Find(state.All)
			if err != nil {
				return err
			}
			for _, n := range nodes {
				if err := tx.Nodes().Delete(n.ID); err != nil {
					return err
				}
			}
			for _, n := range snapshot.Nodes {
				if err := tx.Nodes().Create(n); err != nil {
					return err
				}
			}
			return nil
		},
		ApplyStoreAction: func(tx state.Tx, sa *api.StoreAction) error {
			switch v := sa.Target.(type) {
			case *api.StoreAction_Node:
				obj := v.Node
				ds := tx.Nodes()
				switch sa.Action {
				case api.StoreActionKindCreate:
					return ds.Create(obj)
				case api.StoreActionKindUpdate:
					return ds.Update(obj)
				case api.StoreActionKindRemove:
					return ds.Delete(obj.ID)
				}
			}
			return errUnknownStoreAction
		},
		NewStoreAction: func(c state.Event) (api.StoreAction, error) {
			var sa api.StoreAction
			switch v := c.(type) {
			case state.EventCreateNode:
				sa.Action = api.StoreActionKindCreate
				sa.Target = &api.StoreAction_Node{
					Node: v.Node,
				}
			case state.EventUpdateNode:
				sa.Action = api.StoreActionKindUpdate
				sa.Target = &api.StoreAction_Node{
					Node: v.Node,
				}
			case state.EventDeleteNode:
				sa.Action = api.StoreActionKindRemove
				sa.Target = &api.StoreAction_Node{
					Node: v.Node,
				}
			default:
				return api.StoreAction{}, errUnknownStoreAction
			}
			return sa, nil
		},
	})
}

type nodeEntry struct {
	*api.Node
}

func (n nodeEntry) ID() string {
	return n.Node.ID
}

func (n nodeEntry) Version() api.Version {
	return n.Node.Version
}

func (n nodeEntry) Copy(version *api.Version) state.Object {
	copy := n.Node.Copy()
	if version != nil {
		copy.Version = *version
	}
	return nodeEntry{copy}
}

func (n nodeEntry) EventCreate() state.Event {
	return state.EventCreateNode{Node: n.Node}
}

func (n nodeEntry) EventUpdate() state.Event {
	return state.EventUpdateNode{Node: n.Node}
}

func (n nodeEntry) EventDelete() state.Event {
	return state.EventDeleteNode{Node: n.Node}
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
	err := nodes.tx.create(nodes.table(), nodeEntry{n})
	if err == nil && nodes.tx.curVersion != nil {
		n.Version = *nodes.tx.curVersion
	}
	return err
}

// Update updates an existing node in the store.
// Returns ErrNotExist if the node doesn't exist.
func (nodes nodes) Update(n *api.Node) error {
	return nodes.tx.update(nodes.table(), nodeEntry{n})
}

// Delete removes a node from the store.
// Returns ErrNotExist if the node doesn't exist.
func (nodes nodes) Delete(id string) error {
	return nodes.tx.delete(nodes.table(), id)
}

// Get looks up a node by ID.
// Returns nil if the node doesn't exist.
func (nodes nodes) Get(id string) *api.Node {
	n := get(nodes.memDBTx, nodes.table(), id)
	if n == nil {
		return nil
	}
	return n.(nodeEntry).Node
}

// Find selects a set of nodes and returns them.
func (nodes nodes) Find(by state.By) ([]*api.Node, error) {
	switch by.(type) {
	case state.AllFinder, state.NameFinder, state.QueryFinder:
	default:
		return nil, state.ErrInvalidFindBy
	}

	nodeList := []*api.Node{}
	err := find(nodes.memDBTx, nodes.table(), by, func(o state.Object) {
		nodeList = append(nodeList, o.(nodeEntry).Node)
	})
	return nodeList, err
}

type nodeIndexerByID struct{}

func (ni nodeIndexerByID) FromArgs(args ...interface{}) ([]byte, error) {
	return fromArgs(args...)
}

func (ni nodeIndexerByID) FromObject(obj interface{}) (bool, []byte, error) {
	n, ok := obj.(nodeEntry)
	if !ok {
		panic("unexpected type passed to FromObject")
	}

	// Add the null character as a terminator
	val := n.Node.ID + "\x00"
	return true, []byte(val), nil
}

func (ni nodeIndexerByID) PrefixFromArgs(args ...interface{}) ([]byte, error) {
	return prefixFromArgs(args...)
}

type nodeIndexerByName struct{}

func (ni nodeIndexerByName) FromArgs(args ...interface{}) ([]byte, error) {
	return fromArgs(args...)
}

func (ni nodeIndexerByName) FromObject(obj interface{}) (bool, []byte, error) {
	n, ok := obj.(nodeEntry)
	if !ok {
		panic("unexpected type passed to FromObject")
	}

	if n.Spec == nil {
		return false, nil, nil
	}
	// Add the null character as a terminator
	return true, []byte(n.Spec.Annotations.Name + "\x00"), nil
}
