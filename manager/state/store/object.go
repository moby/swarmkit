package store

import (
	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/manager/state"
	memdb "github.com/hashicorp/go-memdb"
)

// Object is a generic object that can be handled by the store.
type Object interface {
	ID() string               // Get ID
	Version() api.Version     // Retrieve version information
	SetVersion(api.Version)   // Set version information
	Copy(*api.Version) Object // Return a copy of this object, optionally setting new version information on the copy
	EventCreate() state.Event // Return a creation event
	EventUpdate() state.Event // Return an update event
	EventDelete() state.Event // Return a deletion event
}

// ObjectStoreConfig provides the necessary methods to store a particular object
// type inside MemoryStore.
type ObjectStoreConfig struct {
	Name             string
	Table            *memdb.TableSchema
	Save             func(ReadTx, *api.StoreSnapshot) error
	Restore          func(Tx, *api.StoreSnapshot) error
	ApplyStoreAction func(Tx, *api.StoreAction) error
	NewStoreAction   func(state.Event) (api.StoreAction, error)
}
