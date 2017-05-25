package autoindex

import (
	"errors"

	"github.com/docker/swarmkit/api"
	"github.com/docker/swarmkit/manager/state/store/autoindex/protobuf"
)

// An Indexer automatically indexes stored data.
type Indexer interface {
	// Generate indices from the given payload
	Index([]byte) ([]api.IndexEntry, error)
}

// NewIndexer returns an indexer based on the schema and index declarations
// inside spec.
func NewIndexer(spec *api.IndexSpec) (Indexer, error) {
	switch spec.Schema.(type) {
	case *api.IndexSpec_Protobuf:
		return protobuf.NewProtobufIndexer(spec.GetProtobuf(), spec.Indices)
	default:
		return nil, errors.New("unrecognized schema type")
	}
}
