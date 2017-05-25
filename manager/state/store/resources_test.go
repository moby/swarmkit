package store

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io/ioutil"
	"testing"

	"github.com/docker/swarmkit/api"
	"github.com/docker/swarmkit/api/descriptor"
	"github.com/gogo/protobuf/proto"
	"github.com/gogo/protobuf/types"
	"github.com/stretchr/testify/assert"
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
		&api.Node{},       // objects.proto
		&api.NodeSpec{},   // specs.proto
		&api.NodeStatus{}, // types.proto
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

func TestAutoIndex(t *testing.T) {
	s := NewMemoryStore(nil)
	assert.NotNil(t, s)

	descriptors := collectDescriptors()

	extension := &api.Extension{
		ID: "id1",
		Annotations: api.Annotations{
			Name: "genericnode",
		},
		Autoindex: &api.IndexSpec{
			Schema: &api.IndexSpec_Protobuf{
				Protobuf: &api.IndexSpec_ProtobufSchema{
					DescriptorSet: descriptors,
					Type:          "docker.swarmkit.v1.Node",
				},
			},
			Indices: []*api.IndexDeclaration{
				{
					Key:   "state",
					Field: "status.state",
				},
			},
		},
	}

	node1 := &api.Node{
		ID: "node1",
		Status: api.NodeStatus{
			State: api.NodeStatus_UNKNOWN,
		},
		Role: api.NodeRoleWorker,
	}
	marshalledNode1, err := node1.Marshal()
	if err != nil {
		panic(err)
	}

	node2 := &api.Node{
		ID: "node2",
		Status: api.NodeStatus{
			State: api.NodeStatus_DOWN,
		},
		Role: api.NodeRoleWorker,
	}
	marshalledNode2, err := node2.Marshal()
	if err != nil {
		panic(err)
	}

	node3 := &api.Node{
		ID: "node3",
		Status: api.NodeStatus{
			State: api.NodeStatus_READY,
		},
		Role: api.NodeRoleManager,
	}
	marshalledNode3, err := node3.Marshal()
	if err != nil {
		panic(err)
	}

	err = s.Update(func(tx Tx) error {
		// Create an extensionfor protobuf-encoded Node objects
		require.NoError(t, CreateExtension(tx, extension))

		// Create a few node objects using the resource mechanism.
		require.NoError(t, CreateResource(tx,
			&api.Resource{
				ID:   "node1",
				Kind: "genericnode",
				Payload: &types.Any{
					Value: marshalledNode1,
				},
			},
		))

		// Create a few node objects using the generic mechanism.
		require.NoError(t, CreateResource(tx,
			&api.Resource{
				ID:   "node2",
				Kind: "genericnode",
				Payload: &types.Any{
					Value: marshalledNode2,
				},
			},
		))

		// Create a few node objects using the generic mechanism.
		require.NoError(t, CreateResource(tx,
			&api.Resource{
				ID:   "node3",
				Kind: "genericnode",
				Payload: &types.Any{
					Value: marshalledNode3,
				},
			},
		))

		return nil
	})
	assert.NoError(t, err)

	// Test the automatic index
	s.View(func(readTx ReadTx) {
		foundObjs, err := FindResources(readTx, ByCustom("genericnode", "state", "0"))
		assert.NoError(t, err)
		require.Len(t, foundObjs, 1)
		assert.Equal(t, marshalledNode1, foundObjs[0].Payload.Value)

		foundObjs, err = FindResources(readTx, ByCustom("genericnode", "state", "1"))
		assert.NoError(t, err)
		require.Len(t, foundObjs, 1)
		assert.Equal(t, marshalledNode2, foundObjs[0].Payload.Value)

		foundObjs, err = FindResources(readTx, ByCustom("genericnode", "state", "2"))
		assert.NoError(t, err)
		require.Len(t, foundObjs, 1)
		assert.Equal(t, marshalledNode3, foundObjs[0].Payload.Value)
	})
	assert.NoError(t, err)
}
