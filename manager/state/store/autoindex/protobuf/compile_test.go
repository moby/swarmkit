package protobuf

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io/ioutil"
	"testing"

	"github.com/docker/swarmkit/api"
	"github.com/docker/swarmkit/api/descriptor"
	"github.com/gogo/protobuf/proto"
	"github.com/stretchr/testify/require"
)

// extractFile extracts a FileDescriptorProto from a gzip'd buffer.
func extractFile(gz []byte) (*descriptor.FileDescriptorProto, error) {
	r, err := gzip.NewReader(bytes.NewReader(gz))
	if err != nil {
		return nil, fmt.Errorf("failed to open gzip reader: %v", err)
	}
	defer r.Close()

	b, err := ioutil.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("failed to uncompress descriptor: %v", err)
	}

	fd := new(descriptor.FileDescriptorProto)
	if err := proto.Unmarshal(b, fd); err != nil {
		return nil, fmt.Errorf("malformed FileDescriptorProto: %v", err)
	}

	return fd, nil
}

// collectDescriptors returns a FileDescriptorSet which includes a large enough
// subset of the API package to run the tests.
func collectDescriptors() *descriptor.FileDescriptorSet {
	type describable interface {
		Descriptor() ([]byte, []int)
	}

	messages := []describable{
		&api.Task{},       // objects.proto
		&api.TaskSpec{},   // specs.proto
		&api.TaskStatus{}, // types.proto
	}

	var set descriptor.FileDescriptorSet
	for _, m := range messages {
		dGZ, _ := m.Descriptor()

		d, err := extractFile(dGZ)
		if err != nil {
			panic(err)
		}
		set.File = append(set.File, d)
	}

	return &set
}

func TestCompile(t *testing.T) {
	descriptors := collectDescriptors()

	testVecs := []struct {
		schema           api.IndexSpec_ProtobufSchema
		indexDeclaration api.IndexDeclaration
		expected         protobufIndex
	}{
		{
			schema: api.IndexSpec_ProtobufSchema{
				DescriptorSet: descriptors,
				Type:          "docker.swarmkit.v1.Task",
			},
			indexDeclaration: api.IndexDeclaration{
				Key:   "state",
				Field: "status.state",
			},
			expected: protobufIndex{
				key:             "state",
				fieldNumberPath: []int32{9, 2},
				finalType:       descriptor.FieldDescriptorProto_TYPE_ENUM,
				finalLabel:      descriptor.FieldDescriptorProto_LABEL_OPTIONAL,
				defaultValue:    "0",
			},
		},
		{
			schema: api.IndexSpec_ProtobufSchema{
				DescriptorSet: descriptors,
				Type:          "docker.swarmkit.v1.Task",
			},
			indexDeclaration: api.IndexDeclaration{
				Key:   "propagation",
				Field: "spec.container.mounts.bind_options.propagation",
			},
			expected: protobufIndex{
				key:             "propagation",
				fieldNumberPath: []int32{3, 1, 8, 5, 1},
				finalType:       descriptor.FieldDescriptorProto_TYPE_ENUM,
				finalLabel:      descriptor.FieldDescriptorProto_LABEL_OPTIONAL,
				defaultValue:    "0",
			},
		},
		{
			schema: api.IndexSpec_ProtobufSchema{
				DescriptorSet: descriptors,
				Type:          "docker.swarmkit.v1.Mount.BindOptions",
			},
			indexDeclaration: api.IndexDeclaration{
				Key:   "propagation",
				Field: "propagation",
			},
			expected: protobufIndex{
				key:             "propagation",
				fieldNumberPath: []int32{1},
				finalType:       descriptor.FieldDescriptorProto_TYPE_ENUM,
				finalLabel:      descriptor.FieldDescriptorProto_LABEL_OPTIONAL,
				defaultValue:    "0",
			},
		},
	}

	for i, vec := range testVecs {
		indexer, err := NewProtobufIndexer(&vec.schema, []*api.IndexDeclaration{&vec.indexDeclaration})
		require.NoError(t, err, fmt.Sprintf("test case %d", i))

		require.Len(t, indexer.indices, 1)
		require.Equal(t, vec.expected, indexer.indices[0])
	}
}
