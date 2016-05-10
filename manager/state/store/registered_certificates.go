package store

import (
	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/manager/state"
	memdb "github.com/hashicorp/go-memdb"
)

const tableRegisteredCertificate = "registered_certificate"

func init() {
	register(ObjectStoreConfig{
		Name: tableRegisteredCertificate,
		Table: &memdb.TableSchema{
			Name: tableRegisteredCertificate,
			Indexes: map[string]*memdb.IndexSchema{
				indexID: {
					Name:    indexID,
					Unique:  true,
					Indexer: registeredCertificateIndexerByID{},
				},
				indexName: {
					Name:         indexName,
					AllowMissing: true,
					Indexer:      registeredCertificateIndexerByCN{},
				},
			},
		},
		Save: func(tx state.ReadTx, snapshot *api.StoreSnapshot) error {
			var err error
			snapshot.RegisteredCertificates, err = tx.RegisteredCertificates().Find(state.All)
			return err
		},
		Restore: func(tx state.Tx, snapshot *api.StoreSnapshot) error {
			registeredCertificates, err := tx.RegisteredCertificates().Find(state.All)
			if err != nil {
				return err
			}
			for _, n := range registeredCertificates {
				if err := tx.RegisteredCertificates().Delete(n.ID); err != nil {
					return err
				}
			}
			for _, n := range snapshot.RegisteredCertificates {
				if err := tx.RegisteredCertificates().Create(n); err != nil {
					return err
				}
			}
			return nil
		},
		ApplyStoreAction: func(tx state.Tx, sa *api.StoreAction) error {
			switch v := sa.Target.(type) {
			case *api.StoreAction_RegisteredCertificate:
				obj := v.RegisteredCertificate
				ds := tx.RegisteredCertificates()
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
			case state.EventCreateRegisteredCertificate:
				sa.Action = api.StoreActionKindCreate
				sa.Target = &api.StoreAction_RegisteredCertificate{
					RegisteredCertificate: v.RegisteredCertificate,
				}
			case state.EventUpdateRegisteredCertificate:
				sa.Action = api.StoreActionKindUpdate
				sa.Target = &api.StoreAction_RegisteredCertificate{
					RegisteredCertificate: v.RegisteredCertificate,
				}
			case state.EventDeleteRegisteredCertificate:
				sa.Action = api.StoreActionKindRemove
				sa.Target = &api.StoreAction_RegisteredCertificate{
					RegisteredCertificate: v.RegisteredCertificate,
				}
			default:
				return api.StoreAction{}, errUnknownStoreAction
			}
			return sa, nil
		},
	})
}

type registeredCertificateEntry struct {
	*api.RegisteredCertificate
}

func (n registeredCertificateEntry) ID() string {
	return n.RegisteredCertificate.ID
}

func (n registeredCertificateEntry) Version() api.Version {
	return n.RegisteredCertificate.Version
}

func (n registeredCertificateEntry) Copy(version *api.Version) state.Object {
	copy := n.RegisteredCertificate.Copy()
	if version != nil {
		copy.Version = *version
	}
	return registeredCertificateEntry{copy}
}

func (n registeredCertificateEntry) EventCreate() state.Event {
	return state.EventCreateRegisteredCertificate{RegisteredCertificate: n.RegisteredCertificate}
}

func (n registeredCertificateEntry) EventUpdate() state.Event {
	return state.EventUpdateRegisteredCertificate{RegisteredCertificate: n.RegisteredCertificate}
}

func (n registeredCertificateEntry) EventDelete() state.Event {
	return state.EventDeleteRegisteredCertificate{RegisteredCertificate: n.RegisteredCertificate}
}

type registeredCertificates struct {
	tx      *tx
	memDBTx *memdb.Txn
}

func (rc registeredCertificates) table() string {
	return tableRegisteredCertificate
}

// Create adds a new RegisteredCertificate to the store.
// Returns ErrExist if the ID is already taken.
func (rc registeredCertificates) Create(n *api.RegisteredCertificate) error {
	err := rc.tx.create(rc.table(), registeredCertificateEntry{n})
	if err == nil && rc.tx.curVersion != nil {
		n.Version = *rc.tx.curVersion
	}
	return err
}

// Update updates an existing RegisteredCertificate in the store.
// Returns ErrNotExist if the RegisteredCertificate doesn't exist.
func (rc registeredCertificates) Update(n *api.RegisteredCertificate) error {
	return rc.tx.update(rc.table(), registeredCertificateEntry{n})
}

// Delete removes a RegisteredCertificate from the store.
// Returns ErrNotExist if the RegisteredCertificate doesn't exist.
func (rc registeredCertificates) Delete(id string) error {
	return rc.tx.delete(rc.table(), id)
}

// Get looks up a RegisteredCertificate by ID.
// Returns nil if the RegisteredCertificate doesn't exist.
func (rc registeredCertificates) Get(id string) *api.RegisteredCertificate {
	n := get(rc.memDBTx, rc.table(), id)
	if n == nil {
		return nil
	}
	return n.(registeredCertificateEntry).RegisteredCertificate
}

// Find selects a set of RegisteredCertificates and returns them.
func (rc registeredCertificates) Find(by state.By) ([]*api.RegisteredCertificate, error) {
	switch by.(type) {
	case state.AllFinder, state.NameFinder:
	default:
		return nil, state.ErrInvalidFindBy
	}

	registeredCertificateList := []*api.RegisteredCertificate{}
	err := find(rc.memDBTx, rc.table(), by, func(o state.Object) {
		registeredCertificateList = append(registeredCertificateList, o.(registeredCertificateEntry).RegisteredCertificate)
	})
	return registeredCertificateList, err
}

type registeredCertificateIndexerByID struct{}

func (ni registeredCertificateIndexerByID) FromArgs(args ...interface{}) ([]byte, error) {
	return fromArgs(args...)
}

func (ni registeredCertificateIndexerByID) FromObject(obj interface{}) (bool, []byte, error) {
	n, ok := obj.(registeredCertificateEntry)
	if !ok {
		panic("unexpected type passed to FromObject")
	}

	// Add the null character as a terminator
	val := n.RegisteredCertificate.ID + "\x00"
	return true, []byte(val), nil
}

func (ni registeredCertificateIndexerByID) PrefixFromArgs(args ...interface{}) ([]byte, error) {
	return prefixFromArgs(args...)
}

type registeredCertificateIndexerByCN struct{}

func (ni registeredCertificateIndexerByCN) FromArgs(args ...interface{}) ([]byte, error) {
	return fromArgs(args...)
}

func (ni registeredCertificateIndexerByCN) FromObject(obj interface{}) (bool, []byte, error) {
	n, ok := obj.(registeredCertificateEntry)
	if !ok {
		panic("unexpected type passed to FromObject")
	}

	if n.CN == "" {
		return false, nil, nil
	}
	// Add the null character as a terminator
	return true, []byte(n.CN + "\x00"), nil
}
