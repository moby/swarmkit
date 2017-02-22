package store

import (
	"github.com/docker/swarmkit/api"
	memdb "github.com/hashicorp/go-memdb"
	"github.com/pkg/errors"
)

const tableResource = "resource"

func init() {
	register(ObjectStoreConfig{
		Table: &memdb.TableSchema{
			Name: tableResource,
			Indexes: map[string]*memdb.IndexSchema{
				indexID: {
					Name:    indexID,
					Unique:  true,
					Indexer: resourceIndexerByID{},
				},
				indexName: {
					Name:    indexName,
					Unique:  true,
					Indexer: resourceIndexerByName{},
				},
				indexKind: {
					Name:    indexKind,
					Indexer: resourceIndexerByKind{},
				},
				indexCustom: {
					Name:         indexCustom,
					Indexer:      resourceCustomIndexer{},
					AllowMissing: true,
				},
			},
		},
		Save: func(tx ReadTx, snapshot *api.StoreSnapshot) error {
			var err error
			snapshot.Resources, err = FindResources(tx, All)
			return err
		},
		Restore: func(tx Tx, snapshot *api.StoreSnapshot) error {
			resources, err := FindResources(tx, All)
			if err != nil {
				return err
			}
			for _, r := range resources {
				if err := DeleteResource(tx, r.ID); err != nil {
					return err
				}
			}
			for _, r := range snapshot.Resources {
				if err := CreateResource(tx, r); err != nil {
					return err
				}
			}
			return nil
		},
		ApplyStoreAction: func(tx Tx, sa api.StoreAction) error {
			switch v := sa.Target.(type) {
			case *api.StoreAction_Resource:
				obj := v.Resource
				switch sa.Action {
				case api.StoreActionKindCreate:
					return CreateResource(tx, obj)
				case api.StoreActionKindUpdate:
					return UpdateResource(tx, obj)
				case api.StoreActionKindRemove:
					return DeleteResource(tx, obj.ID)
				}
			}
			return errUnknownStoreAction
		},
	})
}

type resourceEntry struct {
	*api.Resource
	extension extensionEntry
}

func (r resourceEntry) CopyStoreObject() api.StoreObject {
	return resourceEntry{Resource: r.Resource.Copy(), extension: r.extension}
}

func getExtension(tx Tx, r *api.Resource) (extensionEntry, error) {
	// There must be an extension corresponding to the Kind field.
	var extensions []extensionEntry
	appendResult := func(o api.StoreObject) {
		extensions = append(extensions, o.(extensionEntry))
	}
	err := tx.find(tableExtension, ByName(r.Kind), func(by By) error { return nil }, appendResult)
	if err != nil {
		return extensionEntry{}, errors.Wrap(err, "failed to query extension")
	}
	if len(extensions) == 0 {
		return extensionEntry{}, errors.Errorf("object kind %s is unregistered", r.Kind)
	}
	if len(extensions) > 1 {
		panic("find by name returned multiple results")
	}
	return extensions[0], nil
}

// CreateResource adds a new resource object to the store.
// Returns ErrExist if the ID is already taken.
func CreateResource(tx Tx, r *api.Resource) error {
	extension, err := getExtension(tx, r)
	if err != nil {
		return err
	}
	return tx.create(tableResource, resourceEntry{Resource: r, extension: extension})
}

// UpdateResource updates an existing resource object in the store.
// Returns ErrNotExist if the object doesn't exist.
func UpdateResource(tx Tx, r *api.Resource) error {
	extension, err := getExtension(tx, r)
	if err != nil {
		return err
	}
	return tx.update(tableResource, resourceEntry{Resource: r, extension: extension})
}

// DeleteResource removes a resource object from the store.
// Returns ErrNotExist if the object doesn't exist.
func DeleteResource(tx Tx, id string) error {
	return tx.delete(tableResource, id)
}

// GetResource looks up a resource object by ID.
// Returns nil if the object doesn't exist.
func GetResource(tx ReadTx, id string) *api.Resource {
	r := tx.get(tableResource, id)
	if r == nil {
		return nil
	}
	return r.(resourceEntry).Resource
}

// FindResources selects a set of resource objects and returns them.
func FindResources(tx ReadTx, by By) ([]*api.Resource, error) {
	checkType := func(by By) error {
		switch by.(type) {
		case byIDPrefix, byName, byKind, byCustom, byCustomPrefix:
			return nil
		default:
			return ErrInvalidFindBy
		}
	}

	resourceList := []*api.Resource{}
	appendResult := func(o api.StoreObject) {
		resourceList = append(resourceList, o.(resourceEntry).Resource)
	}

	err := tx.find(tableResource, by, checkType, appendResult)
	return resourceList, err
}

type resourceIndexerByKind struct{}

func (ri resourceIndexerByKind) FromArgs(args ...interface{}) ([]byte, error) {
	return fromArgs(args...)
}

func (ri resourceIndexerByKind) FromObject(obj interface{}) (bool, []byte, error) {
	r := obj.(resourceEntry)

	// Add the null character as a terminator
	val := r.Resource.Kind + "\x00"
	return true, []byte(val), nil
}

type resourceIndexerByID struct{}

func (indexer resourceIndexerByID) FromArgs(args ...interface{}) ([]byte, error) {
	return api.ResourceIndexerByID{}.FromArgs(args...)
}
func (indexer resourceIndexerByID) PrefixFromArgs(args ...interface{}) ([]byte, error) {
	return api.ResourceIndexerByID{}.PrefixFromArgs(args...)
}
func (indexer resourceIndexerByID) FromObject(obj interface{}) (bool, []byte, error) {
	return api.ResourceIndexerByID{}.FromObject(obj.(resourceEntry).Resource)
}

type resourceIndexerByName struct{}

func (indexer resourceIndexerByName) FromArgs(args ...interface{}) ([]byte, error) {
	return api.ResourceIndexerByName{}.FromArgs(args...)
}
func (indexer resourceIndexerByName) PrefixFromArgs(args ...interface{}) ([]byte, error) {
	return api.ResourceIndexerByName{}.PrefixFromArgs(args...)
}
func (indexer resourceIndexerByName) FromObject(obj interface{}) (bool, []byte, error) {
	return api.ResourceIndexerByName{}.FromObject(obj.(resourceEntry).Resource)
}

type resourceCustomIndexer struct{}

func (indexer resourceCustomIndexer) FromArgs(args ...interface{}) ([]byte, error) {
	return api.ResourceCustomIndexer{}.FromArgs(args...)
}
func (indexer resourceCustomIndexer) PrefixFromArgs(args ...interface{}) ([]byte, error) {
	return api.ResourceCustomIndexer{}.PrefixFromArgs(args...)
}
func (indexer resourceCustomIndexer) FromObject(obj interface{}) (bool, [][]byte, error) {
	r := obj.(resourceEntry)

	var autoIndices [][]byte

	// Automatic index?
	if r.extension.indexer != nil && r.Payload != nil {
		res, err := r.extension.indexer.Index(r.Payload.Value)
		if err != nil {
			return false, nil, err
		}

		for _, entry := range res {
			index := make([]byte, 0, len(r.Kind)+1+len(entry.Key)+1+len(entry.Val)+1)
			index = append(index, []byte(r.Kind)...)
			index = append(index, '|')
			index = append(index, []byte(entry.Key)...)
			index = append(index, '|')
			index = append(index, []byte(entry.Val)...)
			index = append(index, '\x00')
			autoIndices = append(autoIndices, index)
		}
	}

	_, customIndices, _ := api.ResourceCustomIndexer{}.FromObject(r.Resource)

	if len(customIndices) == 0 {
		return len(autoIndices) != 0, autoIndices, nil
	}

	if len(autoIndices) != 0 {
		// TODO(aaronl): There should probably be some protection
		// between conflicting manual and automatic indices.
		customIndices = append(customIndices, autoIndices...)
	}
	return len(customIndices) != 0, customIndices, nil
}
