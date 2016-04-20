package store

import (
	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/manager/state"
	"github.com/docker/swarm-v2/manager/state/pb"
	memdb "github.com/hashicorp/go-memdb"
)

const tableNetwork = "network"

func init() {
	register(ObjectStoreConfig{
		Name: tableNetwork,
		Table: &memdb.TableSchema{
			Name: tableNetwork,
			Indexes: map[string]*memdb.IndexSchema{
				indexID: {
					Name:    indexID,
					Unique:  true,
					Indexer: networkIndexerByID{},
				},
				indexName: {
					Name:    indexName,
					Unique:  true,
					Indexer: networkIndexerByName{},
				},
			},
		},
		Save: func(tx state.ReadTx, snapshot *pb.StoreSnapshot) error {
			var err error
			snapshot.Networks, err = tx.Networks().Find(state.All)
			return err
		},
		Restore: func(tx state.Tx, snapshot *pb.StoreSnapshot) error {
			networks, err := tx.Networks().Find(state.All)
			if err != nil {
				return err
			}
			for _, n := range networks {
				if err := tx.Networks().Delete(n.ID); err != nil {
					return err
				}
			}
			for _, n := range snapshot.Networks {
				if err := tx.Networks().Create(n); err != nil {
					return err
				}
			}
			return nil
		},
		ApplyStoreAction: func(tx state.Tx, sa *api.StoreAction) error {
			switch v := sa.Target.(type) {
			case *api.StoreAction_Network:
				obj := v.Network
				ds := tx.Networks()
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
			case state.EventCreateNetwork:
				sa.Action = api.StoreActionKindCreate
				sa.Target = &api.StoreAction_Network{
					Network: v.Network,
				}
			case state.EventUpdateNetwork:
				sa.Action = api.StoreActionKindUpdate
				sa.Target = &api.StoreAction_Network{
					Network: v.Network,
				}
			case state.EventDeleteNetwork:
				sa.Action = api.StoreActionKindRemove
				sa.Target = &api.StoreAction_Network{
					Network: v.Network,
				}
			default:
				return api.StoreAction{}, errUnknownStoreAction
			}
			return sa, nil
		},
	})
}

type networkEntry struct {
	*api.Network
}

func (n networkEntry) ID() string {
	return n.Network.ID
}

func (n networkEntry) Version() api.Version {
	return n.Network.Version
}

func (n networkEntry) Copy(version *api.Version) state.Object {
	copy := n.Network.Copy()
	if version != nil {
		copy.Version = *version
	}
	return networkEntry{copy}
}

func (n networkEntry) EventCreate() state.Event {
	return state.EventCreateNetwork{Network: n.Network}
}

func (n networkEntry) EventUpdate() state.Event {
	return state.EventUpdateNetwork{Network: n.Network}
}

func (n networkEntry) EventDelete() state.Event {
	return state.EventDeleteNetwork{Network: n.Network}
}

type networks struct {
	tx      *tx
	memDBTx *memdb.Txn
}

func (networks networks) table() string {
	return tableNetwork
}

// Create adds a new network to the store.
// Returns ErrExist if the ID is already taken.
func (networks networks) Create(n *api.Network) error {
	// Ensure the name is not already in use.
	if n.Spec != nil && lookup(networks.memDBTx, networks.table(), indexName, n.Spec.Annotations.Name) != nil {
		return state.ErrNameConflict
	}

	err := networks.tx.create(networks.table(), networkEntry{n})
	if err == nil && networks.tx.curVersion != nil {
		n.Version = *networks.tx.curVersion
	}
	return err
}

// Update updates an existing network in the store.
// Returns ErrNotExist if the network doesn't exist.
func (networks networks) Update(n *api.Network) error {
	// Ensure the name is either not in use or already used by this same Network.
	if existing := lookup(networks.memDBTx, networks.table(), indexName, n.Spec.Annotations.Name); existing != nil {
		if existing.ID() != n.ID {
			return state.ErrNameConflict
		}
	}

	return networks.tx.update(networks.table(), networkEntry{n})
}

// Delete removes a network from the store.
// Returns ErrNotExist if the network doesn't exist.
func (networks networks) Delete(id string) error {
	return networks.tx.delete(networks.table(), id)
}

// Get looks up a network by ID.
// Returns nil if the network doesn't exist.
func (networks networks) Get(id string) *api.Network {
	n := get(networks.memDBTx, networks.table(), id)
	if n == nil {
		return nil
	}
	return n.(networkEntry).Network
}

// Find selects a set of networks and returns them.
func (networks networks) Find(by state.By) ([]*api.Network, error) {
	switch by.(type) {
	case state.AllFinder, state.NameFinder, state.QueryFinder:
	default:
		return nil, state.ErrInvalidFindBy
	}

	networkList := []*api.Network{}
	err := find(networks.memDBTx, networks.table(), by, func(o state.Object) {
		networkList = append(networkList, o.(networkEntry).Network)
	})
	return networkList, err
}

type networkIndexerByID struct{}

func (ni networkIndexerByID) FromArgs(args ...interface{}) ([]byte, error) {
	return fromArgs(args...)
}

func (ni networkIndexerByID) FromObject(obj interface{}) (bool, []byte, error) {
	n, ok := obj.(networkEntry)
	if !ok {
		panic("unexpected type passed to FromObject")
	}

	// Add the null character as a terminator
	val := n.Network.ID + "\x00"
	return true, []byte(val), nil
}

func (ni networkIndexerByID) PrefixFromArgs(args ...interface{}) ([]byte, error) {
	return prefixFromArgs(args...)
}

type networkIndexerByName struct{}

func (ni networkIndexerByName) FromArgs(args ...interface{}) ([]byte, error) {
	return fromArgs(args...)
}

func (ni networkIndexerByName) FromObject(obj interface{}) (bool, []byte, error) {
	n, ok := obj.(networkEntry)
	if !ok {
		panic("unexpected type passed to FromObject")
	}

	// Add the null character as a terminator
	if n.Spec == nil {
		return false, nil, nil
	}
	// Add the null character as a terminator
	return true, []byte(n.Spec.Annotations.Name + "\x00"), nil
}
