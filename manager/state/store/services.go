package store

import (
	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/manager/state"
	"github.com/docker/swarm-v2/manager/state/pb"
	memdb "github.com/hashicorp/go-memdb"
)

const tableService = "service"

func init() {
	register(ObjectStoreConfig{
		Name: tableService,
		Table: &memdb.TableSchema{
			Name: tableService,
			Indexes: map[string]*memdb.IndexSchema{
				indexID: {
					Name:    indexID,
					Unique:  true,
					Indexer: serviceIndexerByID{},
				},
				indexName: {
					Name:    indexName,
					Unique:  true,
					Indexer: serviceIndexerByName{},
				},
			},
		},
		Save: func(tx state.ReadTx, snapshot *pb.StoreSnapshot) error {
			var err error
			snapshot.Services, err = tx.Services().Find(state.All)
			return err
		},
		Restore: func(tx state.Tx, snapshot *pb.StoreSnapshot) error {
			services, err := tx.Services().Find(state.All)
			if err != nil {
				return err
			}
			for _, s := range services {
				if err := tx.Services().Delete(s.ID); err != nil {
					return err
				}
			}
			for _, s := range snapshot.Services {
				if err := tx.Services().Create(s); err != nil {
					return err
				}
			}
			return nil
		},
		ApplyStoreAction: func(tx state.Tx, sa *api.StoreAction) error {
			switch v := sa.Target.(type) {
			case *api.StoreAction_Service:
				obj := v.Service
				ds := tx.Services()
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
			case state.EventCreateService:
				sa.Action = api.StoreActionKindCreate
				sa.Target = &api.StoreAction_Service{
					Service: v.Service,
				}
			case state.EventUpdateService:
				sa.Action = api.StoreActionKindUpdate
				sa.Target = &api.StoreAction_Service{
					Service: v.Service,
				}
			case state.EventDeleteService:
				sa.Action = api.StoreActionKindRemove
				sa.Target = &api.StoreAction_Service{
					Service: v.Service,
				}
			default:
				return api.StoreAction{}, errUnknownStoreAction
			}
			return sa, nil
		},
	})
}

type serviceEntry struct {
	*api.Service
}

func (s serviceEntry) ID() string {
	return s.Service.ID
}

func (s serviceEntry) Version() api.Version {
	return s.Service.Version
}

func (s serviceEntry) Copy(version *api.Version) state.Object {
	copy := s.Service.Copy()
	if version != nil {
		copy.Version = *version
	}
	return serviceEntry{copy}
}

func (s serviceEntry) EventCreate() state.Event {
	return state.EventCreateService{Service: s.Service}
}

func (s serviceEntry) EventUpdate() state.Event {
	return state.EventUpdateService{Service: s.Service}
}

func (s serviceEntry) EventDelete() state.Event {
	return state.EventDeleteService{Service: s.Service}
}

type services struct {
	tx      *tx
	memDBTx *memdb.Txn
}

func (services services) table() string {
	return tableService
}

// Create adds a new service to the store.
// Returns ErrExist if the ID is already taken.
func (services services) Create(s *api.Service) error {
	// Ensure the name is not already in use.
	if s.Spec != nil && lookup(services.memDBTx, services.table(), indexName, s.Spec.Meta.Name) != nil {
		return state.ErrNameConflict
	}

	err := services.tx.create(services.table(), serviceEntry{s})
	if err == nil && services.tx.curVersion != nil {
		s.Version = *services.tx.curVersion
	}
	return err
}

// Update updates an existing service in the store.
// Returns ErrNotExist if the service doesn't exist.
func (services services) Update(s *api.Service) error {
	// Ensure the name is either not in use or already used by this same Service.
	if existing := lookup(services.memDBTx, services.table(), indexName, s.Spec.Meta.Name); existing != nil {
		if existing.ID() != s.ID {
			return state.ErrNameConflict
		}
	}

	return services.tx.update(services.table(), serviceEntry{s})
}

// Delete removes a service from the store.
// Returns ErrNotExist if the service doesn't exist.
func (services services) Delete(id string) error {
	return services.tx.delete(services.table(), id)
}

// Get looks up a service by ID.
// Returns nil if the service doesn't exist.
func (services services) Get(id string) *api.Service {
	s := get(services.memDBTx, services.table(), id)
	if s == nil {
		return nil
	}
	return s.(serviceEntry).Service
}

// Find selects a set of services and returns them.
func (services services) Find(by state.By) ([]*api.Service, error) {
	switch by.(type) {
	case state.AllFinder, state.NameFinder, state.QueryFinder:
	default:
		return nil, state.ErrInvalidFindBy
	}

	serviceList := []*api.Service{}
	err := find(services.memDBTx, services.table(), by, func(o state.Object) {
		serviceList = append(serviceList, o.(serviceEntry).Service)
	})
	return serviceList, err
}

type serviceIndexerByID struct{}

func (si serviceIndexerByID) FromArgs(args ...interface{}) ([]byte, error) {
	return fromArgs(args...)
}

func (si serviceIndexerByID) FromObject(obj interface{}) (bool, []byte, error) {
	s, ok := obj.(serviceEntry)
	if !ok {
		panic("unexpected type passed to FromObject")
	}

	// Add the null character as a terminator
	val := s.Service.ID + "\x00"
	return true, []byte(val), nil
}

func (si serviceIndexerByID) PrefixFromArgs(args ...interface{}) ([]byte, error) {
	return prefixFromArgs(args...)
}

type serviceIndexerByName struct{}

func (si serviceIndexerByName) FromArgs(args ...interface{}) ([]byte, error) {
	return fromArgs(args...)
}

func (si serviceIndexerByName) FromObject(obj interface{}) (bool, []byte, error) {
	s, ok := obj.(serviceEntry)
	if !ok {
		panic("unexpected type passed to FromObject")
	}

	// Add the null character as a terminator
	if s.Spec == nil {
		return false, nil, nil
	}
	// Add the null character as a terminator
	return true, []byte(s.Spec.Meta.Name + "\x00"), nil
}
