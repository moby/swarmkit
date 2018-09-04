package store

import (
	"strings"

	"github.com/docker/swarmkit/api"
	memdb "github.com/hashicorp/go-memdb"
)

const tableDeviceClass = "deviceClass"

func init() {
	register(ObjectStoreConfig{
		Table: &memdb.TableSchema{
			Name: tableDeviceClass,
			Indexes: map[string]*memdb.IndexSchema{
				indexID: {
					Name:    indexID,
					Unique:  true,
					Indexer: api.DeviceClassIndexerByID{},
				},
				indexName: {
					Name:    indexName,
					Unique:  true,
					Indexer: api.DeviceClassIndexerByName{},
				},
				indexCustom: {
					Name:         indexCustom,
					Indexer:      api.DeviceClassCustomIndexer{},
					AllowMissing: true,
				},
			},
		},
		Save: func(tx ReadTx, snapshot *api.StoreSnapshot) error {
			var err error
			snapshot.DeviceClasses, err = FindDeviceClasses(tx, All)
			return err
		},
		Restore: func(tx Tx, snapshot *api.StoreSnapshot) error {
			toStoreObj := make([]api.StoreObject, len(snapshot.DeviceClasses))
			for i, x := range snapshot.DeviceClasses {
				toStoreObj[i] = x
			}
			return RestoreTable(tx, tableDeviceClass, toStoreObj)
		},
		ApplyStoreAction: func(tx Tx, sa api.StoreAction) error {
			switch v := sa.Target.(type) {
			case *api.StoreAction_DeviceClass:
				obj := v.DeviceClass
				switch sa.Action {
				case api.StoreActionKindCreate:
					return CreateDeviceClass(tx, obj)
				case api.StoreActionKindUpdate:
					return UpdateDeviceClass(tx, obj)
				case api.StoreActionKindRemove:
					return DeleteDeviceClass(tx, obj.ID)
				}
			}
			return errUnknownStoreAction
		},
	})
}

// CreateDeviceClass adds a new DeviceClass to the store.
// Returns:
//   - ErrExist if the ID is already taken
//   - ErrNameConflict is a DeviceClass with this name already exists.
func CreateDeviceClass(tx Tx, dc *api.DeviceClass) error {
	// ensure the name is not already in use
	if tx.lookup(tableDeviceClass, indexName, strings.ToLower(dc.Spec.Annotations.Name)) != nil {
		return ErrNameConflict
	}

	return tx.create(tableDeviceClass, dc)
}

// UpdateDeviceClass updates an existing DeviceClass in the store.
// Returns:
//   - ErrNotExist if the device class doesn't exist.
//   - ErrNameConflict if the name of the DeviceClass is being changed and
//     there already exists another DeviceClass with that name
func UpdateDeviceClass(tx Tx, dc *api.DeviceClass) error {
	if existing := tx.lookup(tableDeviceClass, indexName, strings.ToLower(dc.Spec.Annotations.Name)); existing != nil {
		if existing.GetID() != dc.ID {
			return ErrNameConflict
		}
	}

	return tx.update(tableDeviceClass, dc)
}

// DeleteDeviceClass removes a DeviceClass from the store.
// Returns ErrNotExist if the DeviceClass doesn't exist.
func DeleteDeviceClass(tx Tx, id string) error {
	return tx.delete(tableDeviceClass, id)
}

// FindDeviceClasses selects a set of DeviceClasses and returns them
// Returns ErrInvalidFindBy if the By isn't a supported index for DeviceClass
func FindDeviceClasses(tx ReadTx, by By) ([]*api.DeviceClass, error) {
	checkType := func(by By) error {
		switch by.(type) {
		case byName, byNamePrefix, byIDPrefix, byCustom, byCustomPrefix:
			return nil
		default:
			return ErrInvalidFindBy
		}
	}

	deviceClassList := []*api.DeviceClass{}
	appendResult := func(o api.StoreObject) {
		deviceClassList = append(deviceClassList, o.(*api.DeviceClass))
	}

	err := tx.find(tableDeviceClass, by, checkType, appendResult)
	return deviceClassList, err
}
