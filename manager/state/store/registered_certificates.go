package store

import (
	"strconv"

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
				indexCN: {
					Name:         indexCN,
					AllowMissing: true,
					Indexer:      registeredCertificateIndexerByCN{},
				},
				indexIssuanceState: {
					Name:         indexIssuanceState,
					AllowMissing: true,
					Indexer:      registeredCertificateIndexerByIssuanceState{},
				},
			},
		},
		Save: func(tx ReadTx, snapshot *api.StoreSnapshot) error {
			var err error
			snapshot.RegisteredCertificates, err = FindRegisteredCertificates(tx, All)
			return err
		},
		Restore: func(tx Tx, snapshot *api.StoreSnapshot) error {
			registeredCertificates, err := FindRegisteredCertificates(tx, All)
			if err != nil {
				return err
			}
			for _, n := range registeredCertificates {
				if err := DeleteRegisteredCertificate(tx, n.ID); err != nil {
					return err
				}
			}
			for _, n := range snapshot.RegisteredCertificates {
				if err := CreateRegisteredCertificate(tx, n); err != nil {
					return err
				}
			}
			return nil
		},
		ApplyStoreAction: func(tx Tx, sa *api.StoreAction) error {
			switch v := sa.Target.(type) {
			case *api.StoreAction_RegisteredCertificate:
				obj := v.RegisteredCertificate
				switch sa.Action {
				case api.StoreActionKindCreate:
					return CreateRegisteredCertificate(tx, obj)
				case api.StoreActionKindUpdate:
					return UpdateRegisteredCertificate(tx, obj)
				case api.StoreActionKindRemove:
					return DeleteRegisteredCertificate(tx, obj.ID)
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

func (n registeredCertificateEntry) Meta() api.Meta {
	return n.RegisteredCertificate.Meta
}

func (n registeredCertificateEntry) SetMeta(meta api.Meta) {
	n.RegisteredCertificate.Meta = meta
}

func (n registeredCertificateEntry) Copy() Object {
	return registeredCertificateEntry{n.RegisteredCertificate.Copy()}
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

// CreateRegisteredCertificate adds a new RegisteredCertificate to the store.
// Returns ErrExist if the ID is already taken.
func CreateRegisteredCertificate(tx Tx, n *api.RegisteredCertificate) error {
	return tx.create(tableRegisteredCertificate, registeredCertificateEntry{n})
}

// UpdateRegisteredCertificate updates an existing RegisteredCertificate in the store.
// Returns ErrNotExist if the RegisteredCertificate doesn't exist.
func UpdateRegisteredCertificate(tx Tx, n *api.RegisteredCertificate) error {
	return tx.update(tableRegisteredCertificate, registeredCertificateEntry{n})
}

// DeleteRegisteredCertificate removes a RegisteredCertificate from the store.
// Returns ErrNotExist if the RegisteredCertificate doesn't exist.
func DeleteRegisteredCertificate(tx Tx, id string) error {
	return tx.delete(tableRegisteredCertificate, id)
}

// GetRegisteredCertificate looks up a RegisteredCertificate by ID.
// Returns nil if the RegisteredCertificate doesn't exist.
func GetRegisteredCertificate(tx ReadTx, id string) *api.RegisteredCertificate {
	n := tx.get(tableRegisteredCertificate, id)
	if n == nil {
		return nil
	}
	return n.(registeredCertificateEntry).RegisteredCertificate
}

// FindRegisteredCertificates selects a set of RegisteredCertificates and returns them.
func FindRegisteredCertificates(tx ReadTx, by By) ([]*api.RegisteredCertificate, error) {
	switch by.(type) {
	case byAll, byCN, byIssuanceState:
	default:
		return nil, ErrInvalidFindBy
	}

	registeredCertificateList := []*api.RegisteredCertificate{}
	err := tx.find(tableRegisteredCertificate, by, func(o Object) {
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

type registeredCertificateIndexerByIssuanceState struct{}

func (ni registeredCertificateIndexerByIssuanceState) FromArgs(args ...interface{}) ([]byte, error) {
	return fromArgs(args...)
}

func (ni registeredCertificateIndexerByIssuanceState) FromObject(obj interface{}) (bool, []byte, error) {
	n, ok := obj.(registeredCertificateEntry)
	if !ok {
		panic("unexpected type passed to FromObject")
	}

	if n.CN == "" {
		return false, nil, nil
	}
	// Add the null character as a terminator
	return true, []byte(strconv.FormatInt(int64(n.Status.State), 10) + "\x00"), nil
}
