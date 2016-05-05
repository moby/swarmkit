package store

import (
	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/manager/state"
	memdb "github.com/hashicorp/go-memdb"
)

const tableVolume = "volume"

func init() {
	register(ObjectStoreConfig{
		Name: tableVolume,
		Table: &memdb.TableSchema{
			Name: tableVolume,
			Indexes: map[string]*memdb.IndexSchema{
				indexID: {
					Name:    indexID,
					Unique:  true,
					Indexer: volumeIndexerByID{},
				},
				indexName: {
					Name:    indexName,
					Unique:  true,
					Indexer: volumeIndexerByName{},
				},
			},
		},
		Save: func(tx ReadTx, snapshot *api.StoreSnapshot) error {
			var err error
			snapshot.Volumes, err = FindVolumes(tx, All)
			return err
		},
		Restore: func(tx Tx, snapshot *api.StoreSnapshot) error {
			volumes, err := FindVolumes(tx, All)
			if err != nil {
				return err
			}
			for _, v := range volumes {
				if err := DeleteVolume(tx, v.ID); err != nil {
					return err
				}
			}
			for _, v := range snapshot.Volumes {
				if err := CreateVolume(tx, v); err != nil {
					return err
				}
			}
			return nil
		},
		ApplyStoreAction: func(tx Tx, sa *api.StoreAction) error {
			switch v := sa.Target.(type) {
			case *api.StoreAction_Volume:
				obj := v.Volume
				switch sa.Action {
				case api.StoreActionKindCreate:
					return CreateVolume(tx, obj)
				case api.StoreActionKindUpdate:
					return UpdateVolume(tx, obj)
				case api.StoreActionKindRemove:
					return DeleteVolume(tx, obj.ID)
				}
			}
			return errUnknownStoreAction
		},
		NewStoreAction: func(c state.Event) (api.StoreAction, error) {
			var sa api.StoreAction
			switch v := c.(type) {
			case state.EventCreateVolume:
				sa.Action = api.StoreActionKindCreate
				sa.Target = &api.StoreAction_Volume{
					Volume: v.Volume,
				}
			case state.EventUpdateVolume:
				sa.Action = api.StoreActionKindUpdate
				sa.Target = &api.StoreAction_Volume{
					Volume: v.Volume,
				}
			case state.EventDeleteVolume:
				sa.Action = api.StoreActionKindRemove
				sa.Target = &api.StoreAction_Volume{
					Volume: v.Volume,
				}
			default:
				return api.StoreAction{}, errUnknownStoreAction
			}
			return sa, nil
		},
	})
}

type volumeEntry struct {
	*api.Volume
}

func (v volumeEntry) ID() string {
	return v.Volume.ID
}

func (v volumeEntry) Meta() api.Meta {
	return v.Volume.Meta
}

func (v volumeEntry) SetMeta(meta api.Meta) {
	v.Volume.Meta = meta
}

func (v volumeEntry) Copy() Object {
	return volumeEntry{v.Volume.Copy()}
}

func (v volumeEntry) EventCreate() state.Event {
	return state.EventCreateVolume{Volume: v.Volume}
}

func (v volumeEntry) EventUpdate() state.Event {
	return state.EventUpdateVolume{Volume: v.Volume}
}

func (v volumeEntry) EventDelete() state.Event {
	return state.EventDeleteVolume{Volume: v.Volume}
}

// CreateVolume adds a new volume to the store.
// Returns ErrExist if the ID is already taken.
func CreateVolume(tx Tx, v *api.Volume) error {
	// Ensure the name is not already in use.
	if tx.lookup(tableVolume, indexName, v.Spec.Annotations.Name) != nil {
		return ErrNameConflict
	}

	return tx.create(tableVolume, volumeEntry{v})
}

// UpdateVolume updates an existing volume in the store.
// Returns ErrNotExist if the volume doesn't exist.
func UpdateVolume(tx Tx, v *api.Volume) error {
	// Ensure the name is either not in use or already used by this same Service.
	if existing := tx.lookup(tableVolume, indexName, v.Spec.Annotations.Name); existing != nil {
		if existing.ID() != v.ID {
			return ErrNameConflict
		}
	}

	return tx.update(tableVolume, volumeEntry{v})
}

// DeleteVolume removes a volume from the store.
// Returns ErrNotExist if the volume doesn't exist.
func DeleteVolume(tx Tx, id string) error {
	return tx.delete(tableVolume, id)
}

// GetVolume looks up a volume by ID.
// Returns nil if the volume doesn't exist.
func GetVolume(tx ReadTx, id string) *api.Volume {
	v := tx.get(tableVolume, id)
	if v == nil {
		return nil
	}
	return v.(volumeEntry).Volume
}

// FindVolumes selects a set of volumes and returns them.
func FindVolumes(tx ReadTx, by By) ([]*api.Volume, error) {
	switch by.(type) {
	case byAll, byName:
	default:
		return nil, ErrInvalidFindBy
	}

	volumeList := []*api.Volume{}
	err := tx.find(tableVolume, by, func(o Object) {
		volumeList = append(volumeList, o.(volumeEntry).Volume)
	})
	return volumeList, err
}

type volumeIndexerByID struct{}

func (vi volumeIndexerByID) FromArgs(args ...interface{}) ([]byte, error) {
	return fromArgs(args...)
}

func (vi volumeIndexerByID) FromObject(obj interface{}) (bool, []byte, error) {
	v, ok := obj.(volumeEntry)
	if !ok {
		panic("unexpected type passed to FromObject")
	}

	// Add the null character as a terminator
	val := v.Volume.ID + "\x00"
	return true, []byte(val), nil
}

type volumeIndexerByName struct{}

func (vi volumeIndexerByName) FromArgs(args ...interface{}) ([]byte, error) {
	return fromArgs(args...)
}

func (vi volumeIndexerByName) FromObject(obj interface{}) (bool, []byte, error) {
	v, ok := obj.(volumeEntry)
	if !ok {
		panic("unexpected type passed to FromObject")
	}

	// Add the null character as a terminator
	return true, []byte(v.Spec.Annotations.Name + "\x00"), nil
}
