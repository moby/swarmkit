package protobuf

import (
	"github.com/docker/swarmkit/api"
	"github.com/docker/swarmkit/api/descriptor"
	"github.com/pkg/errors"
)

type protobufIndex struct {
	key             string
	fieldNumberPath []int32
	finalType       descriptor.FieldDescriptorProto_Type
	finalLabel      descriptor.FieldDescriptorProto_Label
	defaultValue    string
}

// Indexer automatically indexes stored protobuf content.
type Indexer struct {
	indices []protobufIndex
}

// NewProtobufIndexer creates a protobuf indexer from a schema and a set of
// index declarations.
func NewProtobufIndexer(schema *api.IndexSpec_ProtobufSchema, indices []*api.IndexDeclaration) (*Indexer, error) {
	if schema.DescriptorSet == nil {
		return nil, errors.New("no FileDescriptorSet provided")
	}

	indexer := &Indexer{}

	if err := indexer.compile(schema.DescriptorSet, schema.Type, indices); err != nil {
		return nil, err
	}

	return indexer, nil
}
