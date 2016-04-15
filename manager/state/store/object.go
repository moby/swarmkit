package store

import (
	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/manager/state"
	"github.com/docker/swarm-v2/manager/state/pb"
	memdb "github.com/hashicorp/go-memdb"
)

// ObjectStoreConfig provides the necessary methods to store a particular object
// type inside MemoryStore.
type ObjectStoreConfig struct {
	Name             string
	Table            *memdb.TableSchema
	Save             func(state.ReadTx, *pb.StoreSnapshot) error
	Restore          func(state.Tx, *pb.StoreSnapshot) error
	ApplyStoreAction func(state.Tx, *api.StoreAction) error
	NewStoreAction   func(state.Event) (api.StoreAction, error)
}
