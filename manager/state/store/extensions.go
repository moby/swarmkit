package store

import (
	"errors"
	"strings"

	"github.com/docker/swarmkit/api"
	"github.com/docker/swarmkit/manager/state"
	memdb "github.com/hashicorp/go-memdb"
)

const tableExtension = "extension"

func init() {
	register(ObjectStoreConfig{
		Name: tableExtension,
		Table: &memdb.TableSchema{
			Name: tableExtension,
			Indexes: map[string]*memdb.IndexSchema{
				indexID: {
					Name:    indexID,
					Unique:  true,
					Indexer: extensionIndexerByID{},
				},
				indexName: {
					Name:    indexName,
					Unique:  true,
					Indexer: extensionIndexerByName{},
				},
				indexCustom: {
					Name:         indexCustom,
					Indexer:      extensionCustomIndexer{},
					AllowMissing: true,
				},
			},
		},
		Save: func(tx ReadTx, snapshot *api.StoreSnapshot) error {
			var err error
			snapshot.Extensions, err = FindExtensions(tx, All)
			return err
		},
		Restore: func(tx Tx, snapshot *api.StoreSnapshot) error {
			extensions, err := FindExtensions(tx, All)
			if err != nil {
				return err
			}
			for _, e := range extensions {
				if err := DeleteExtension(tx, e.ID); err != nil {
					return err
				}
			}
			for _, e := range snapshot.Extensions {
				if err := CreateExtension(tx, e); err != nil {
					return err
				}
			}
			return nil
		},
		Object: func(sa *api.StoreAction) (Object, error) {
			if extension, ok := sa.Target.(*api.StoreAction_Extension); ok {
				return extensionEntry{Extension: extension.Extension}, nil
			}
			return nil, errUnknownStoreAction
		},
		ApplyStoreAction: func(tx Tx, sa *api.StoreAction) error {
			switch v := sa.Target.(type) {
			case *api.StoreAction_Extension:
				obj := v.Extension
				switch sa.Action {
				case api.StoreActionKindCreate:
					return CreateExtension(tx, obj)
				case api.StoreActionKindUpdate:
					return UpdateExtension(tx, obj)
				case api.StoreActionKindRemove:
					return DeleteExtension(tx, obj.ID)
				}
			}
			return errUnknownStoreAction
		},
		NewStoreAction: func(c state.Event) (api.StoreAction, error) {
			var sa api.StoreAction
			switch v := c.(type) {
			case state.EventCreateExtension:
				sa.Action = api.StoreActionKindCreate
				sa.Target = &api.StoreAction_Extension{
					Extension: v.Extension,
				}
			case state.EventUpdateExtension:
				sa.Action = api.StoreActionKindUpdate
				sa.Target = &api.StoreAction_Extension{
					Extension: v.Extension,
				}
			case state.EventDeleteExtension:
				sa.Action = api.StoreActionKindRemove
				sa.Target = &api.StoreAction_Extension{
					Extension: v.Extension,
				}
			default:
				return api.StoreAction{}, errUnknownStoreAction
			}
			return sa, nil
		},
	})
}

type extensionEntry struct {
	*api.Extension
}

func (e extensionEntry) ID() string {
	return e.Extension.ID
}

func (e extensionEntry) Meta() api.Meta {
	return e.Extension.Meta
}

func (e extensionEntry) SetMeta(meta api.Meta) {
	e.Extension.Meta = meta
}

func (e extensionEntry) Copy() Object {
	return extensionEntry{e.Extension.Copy()}
}

func (e extensionEntry) EventCreate() state.Event {
	return state.EventCreateExtension{Extension: e.Extension}
}

func (e extensionEntry) EventUpdate() state.Event {
	return state.EventUpdateExtension{Extension: e.Extension}
}

func (e extensionEntry) EventDelete() state.Event {
	return state.EventDeleteExtension{Extension: e.Extension}
}

// CreateExtension adds a new extension to the store.
// Returns ErrExist if the ID is already taken.
func CreateExtension(tx Tx, e *api.Extension) error {
	// Ensure the name is not already in use.
	if tx.lookup(tableExtension, indexName, strings.ToLower(e.Annotations.Name)) != nil {
		return ErrNameConflict
	}

	// It can't conflict with built-in kinds either.
	if _, ok := schema.Tables[e.Annotations.Name]; ok {
		return ErrNameConflict
	}

	return tx.create(tableExtension, extensionEntry{e})
}

// UpdateExtension updates an existing extension in the store.
// Returns ErrNotExist if the object doesn't exist.
func UpdateExtension(tx Tx, e *api.Extension) error {
	// TODO(aaronl): For the moment, extensions are immutable
	return errors.New("extensions are immutable")

	// Ensure the name is either not in use or already used by this same Extension.
	/*if existing := tx.lookup(tableExtension, indexName, strings.ToLower(o.Annotations.Name)); existing != nil {
		if existing.ID() != e.ID {
			return ErrNameConflict
		}
	}

	return tx.update(tableExtension, extensionEntry{e})*/
}

// DeleteExtension removes an extension from the store.
// Returns ErrNotExist if the object doesn't exist.
func DeleteExtension(tx Tx, id string) error {
	o := tx.get(tableExtension, id)
	if o == nil {
		return ErrNotExist
	}

	resources, err := FindResources(tx, ByKind(o.(extensionEntry).Annotations.Name))
	if err != nil {
		return err
	}

	if len(resources) != 0 {
		return errors.New("cannot delete extension because objects of this type exist in the data store")
	}

	return tx.delete(tableExtension, id)
}

// GetExtension looks up an extension by ID.
// Returns nil if the object doesn't exist.
func GetExtension(tx ReadTx, id string) *api.Extension {
	e := tx.get(tableExtension, id)
	if e == nil {
		return nil
	}
	return e.(extensionEntry).Extension
}

// FindExtensions selects a set of extensions and returns them.
func FindExtensions(tx ReadTx, by By) ([]*api.Extension, error) {
	checkType := func(by By) error {
		switch by.(type) {
		case byIDPrefix, byName, byCustom, byCustomPrefix:
			return nil
		default:
			return ErrInvalidFindBy
		}
	}

	extensionList := []*api.Extension{}
	appendResult := func(o Object) {
		extensionList = append(extensionList, o.(extensionEntry).Extension)
	}

	err := tx.find(tableExtension, by, checkType, appendResult)
	return extensionList, err
}

type extensionIndexerByID struct{}

func (ei extensionIndexerByID) FromArgs(args ...interface{}) ([]byte, error) {
	return fromArgs(args...)
}

func (ei extensionIndexerByID) FromObject(obj interface{}) (bool, []byte, error) {
	e, ok := obj.(extensionEntry)
	if !ok {
		panic("unexpected type passed to FromObject")
	}

	// Add the null character as a terminator
	val := e.Extension.ID + "\x00"
	return true, []byte(val), nil
}

func (ei extensionIndexerByID) PrefixFromArgs(args ...interface{}) ([]byte, error) {
	return prefixFromArgs(args...)
}

type extensionIndexerByName struct{}

func (ei extensionIndexerByName) FromArgs(args ...interface{}) ([]byte, error) {
	return fromArgs(args...)
}

func (ei extensionIndexerByName) FromObject(obj interface{}) (bool, []byte, error) {
	e, ok := obj.(extensionEntry)
	if !ok {
		panic("unexpected type passed to FromObject")
	}

	// Add the null character as a terminator
	return true, []byte(strings.ToLower(e.Annotations.Name) + "\x00"), nil
}

func (ei extensionIndexerByName) PrefixFromArgs(args ...interface{}) ([]byte, error) {
	return prefixFromArgs(args...)
}

type extensionCustomIndexer struct{}

func (ei extensionCustomIndexer) FromArgs(args ...interface{}) ([]byte, error) {
	return fromArgs(args...)
}

func (ei extensionCustomIndexer) FromObject(obj interface{}) (bool, [][]byte, error) {
	e, ok := obj.(extensionEntry)
	if !ok {
		panic("unexpected type passed to FromObject")
	}

	return customIndexer("", &e.Annotations)
}

func (ei extensionCustomIndexer) PrefixFromArgs(args ...interface{}) ([]byte, error) {
	return prefixFromArgs(args...)
}
