package store

import (
	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/manager/state"
	"github.com/docker/swarm-v2/manager/state/pb"
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
		Save: func(tx state.ReadTx, snapshot *pb.StoreSnapshot) error {
			var err error
			snapshot.Volumes, err = tx.Volumes().Find(state.All)
			return err
		},
		Restore: func(tx state.Tx, snapshot *pb.StoreSnapshot) error {
			volumes, err := tx.Volumes().Find(state.All)
			if err != nil {
				return err
			}
			for _, v := range volumes {
				if err := tx.Volumes().Delete(v.ID); err != nil {
					return err
				}
			}
			for _, v := range snapshot.Volumes {
				if err := tx.Volumes().Create(v); err != nil {
					return err
				}
			}
			return nil
		},
		ApplyStoreAction: func(tx state.Tx, sa *api.StoreAction) error {
			switch v := sa.Target.(type) {
			case *api.StoreAction_Volume:
				obj := v.Volume
				ds := tx.Volumes()
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

func (v volumeEntry) Version() api.Version {
	return v.Volume.Version
}

func (v volumeEntry) Copy(version *api.Version) state.Object {
	copy := v.Volume.Copy()
	if version != nil {
		copy.Version = *version
	}
	return volumeEntry{copy}
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

type volumes struct {
	tx      *tx
	memDBTx *memdb.Txn
}

func (volumes volumes) table() string {
	return tableVolume
}

// Create adds a new volume to the store.
// Returns ErrExist if the ID is already taken.
func (volumes volumes) Create(v *api.Volume) error {
	// Ensure the name is not already in use.
	if lookup(volumes.memDBTx, volumes.table(), indexName, v.Spec.Annotations.Name) != nil {
		return state.ErrNameConflict
	}

	err := volumes.tx.create(volumes.table(), volumeEntry{v})
	if err == nil && volumes.tx.curVersion != nil {
		v.Version = *volumes.tx.curVersion
	}
	return err
}

// Update updates an existing volume in the store.
// Returns ErrNotExist if the volume doesn't exist.
func (volumes volumes) Update(v *api.Volume) error {
	// Ensure the name is either not in use or already used by this same Service.
	if existing := lookup(volumes.memDBTx, volumes.table(), indexName, v.Spec.Annotations.Name); existing != nil {
		if existing.ID() != v.ID {
			return state.ErrNameConflict
		}
	}

	return volumes.tx.update(volumes.table(), volumeEntry{v})
}

// Delete removes a volume from the store.
// Returns ErrNotExist if the volume doesn't exist.
func (volumes volumes) Delete(id string) error {
	return volumes.tx.delete(volumes.table(), id)
}

// Get looks up a volume by ID.
// Returns nil if the volume doesn't exist.
func (volumes volumes) Get(id string) *api.Volume {
	v := get(volumes.memDBTx, volumes.table(), id)
	if v == nil {
		return nil
	}
	return v.(volumeEntry).Volume
}

// Find selects a set of volumes and returns them.
func (volumes volumes) Find(by state.By) ([]*api.Volume, error) {
	switch by.(type) {
	case state.AllFinder, state.NameFinder:
	default:
		return nil, state.ErrInvalidFindBy
	}

	volumeList := []*api.Volume{}
	err := find(volumes.memDBTx, volumes.table(), by, func(o state.Object) {
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
