package protobuf

import (
	"fmt"
	"testing"
	"time"

	"github.com/docker/swarmkit/api"
	"github.com/docker/swarmkit/api/descriptor"
	"github.com/docker/swarmkit/protobuf/ptypes"
	"github.com/stretchr/testify/require"
)

func TestIndex(t *testing.T) {
	task := api.Task{
		ID:        "abc",
		NodeID:    "1234",
		ServiceID: "xyz",
		Status: api.TaskStatus{
			Timestamp: ptypes.MustTimestampProto(time.Now()),
			State:     api.TaskStateRunning,
			Message:   "hi",
		},
		Spec: api.TaskSpec{
			Runtime: &api.TaskSpec_Container{
				Container: &api.ContainerSpec{
					Image:   "foo",
					Command: []string{"abc", "def"},
					Mounts: []api.Mount{
						{
							BindOptions: &api.Mount_BindOptions{
								Propagation: api.MountPropagationRSlave,
							},
						},
					},
				},
			},
		},
	}
	marshalledTask, err := task.Marshal()
	if err != nil {
		panic(err)
	}

	service := api.Service{
		ID: "abc",
		Meta: api.Meta{
			Version: api.Version{
				Index: 102352343,
			},
		},
		Spec: api.ServiceSpec{
			Task: api.TaskSpec{
				Placement: &api.Placement{
					Constraints: []string{
						"constraint1",
						"constraint2",
						"constraint3",
					},
				},
			},
		},
	}
	marshalledService, err := service.Marshal()
	if err != nil {
		panic(err)
	}

	endpoint := api.Endpoint{
		Ports: []*api.PortConfig{
			{
				Name:          "port 1",
				Protocol:      api.ProtocolTCP,
				TargetPort:    1234,
				PublishedPort: 21234,
			},
			{
				Name:          "port 2",
				Protocol:      api.ProtocolTCP,
				TargetPort:    1235,
				PublishedPort: 21235,
			},
			{
				Name:          "port 3",
				Protocol:      api.ProtocolUDP,
				TargetPort:    1236,
				PublishedPort: 21236,
			},
		},
	}
	marshalledEndpoint, err := endpoint.Marshal()
	if err != nil {
		panic(err)
	}

	testVecs := []struct {
		payload  []byte
		index    protobufIndex
		expected []api.IndexEntry
	}{
		{
			payload: marshalledTask,
			// status.state
			index: protobufIndex{
				key:             "state",
				fieldNumberPath: []int32{9, 2},
				finalType:       descriptor.FieldDescriptorProto_TYPE_ENUM,
			},
			expected: []api.IndexEntry{{Key: "state", Val: "512"}},
		},
		{
			payload: marshalledTask,
			// spec.container.mounts.bind_options.propagation
			index: protobufIndex{
				key:             "propagation",
				fieldNumberPath: []int32{3, 1, 8, 5, 1},
				finalType:       descriptor.FieldDescriptorProto_TYPE_ENUM,
			},
			expected: []api.IndexEntry{{Key: "propagation", Val: "4"}},
		},
		{
			payload: marshalledTask,
			// status.message
			index: protobufIndex{
				key:             "message",
				fieldNumberPath: []int32{9, 3},
				finalType:       descriptor.FieldDescriptorProto_TYPE_STRING,
			},
			expected: []api.IndexEntry{{Key: "message", Val: "hi"}},
		},
		{
			payload: marshalledTask,
			// nonexistent field in Task
			index: protobufIndex{
				key:             "nonexistentenum",
				fieldNumberPath: []int32{83, 2},
				finalType:       descriptor.FieldDescriptorProto_TYPE_ENUM,
				defaultValue:    "0",
			},
			expected: []api.IndexEntry{{Key: "nonexistentenum", Val: "0"}},
		},
		{
			payload: marshalledTask,
			// nonexistent field in Task.status
			index: protobufIndex{
				key:             "nonexistentstring",
				fieldNumberPath: []int32{9, 13},
				finalType:       descriptor.FieldDescriptorProto_TYPE_STRING,
			},
			expected: []api.IndexEntry{{Key: "nonexistentstring", Val: ""}},
		},
		{
			payload: marshalledService,
			// repeated field Service.spec.task_spec.placement.constraints
			index: protobufIndex{
				key:             "constraint",
				fieldNumberPath: []int32{3, 2, 5, 1},
				finalType:       descriptor.FieldDescriptorProto_TYPE_STRING,
			},
			expected: []api.IndexEntry{
				{Key: "constraint", Val: "constraint1"},
				{Key: "constraint", Val: "constraint2"},
				{Key: "constraint", Val: "constraint3"},
			},
		},
		{
			payload: marshalledEndpoint,
			// target_port field inside repeated field ports
			index: protobufIndex{
				key:             "publishedport",
				fieldNumberPath: []int32{2, 4},
				finalType:       descriptor.FieldDescriptorProto_TYPE_UINT32,
			},
			expected: []api.IndexEntry{
				{Key: "publishedport", Val: "21234"},
				{Key: "publishedport", Val: "21235"},
				{Key: "publishedport", Val: "21236"},
			},
		},
	}

	for i, vec := range testVecs {
		indexer := Indexer{indices: []protobufIndex{vec.index}}
		entries, err := indexer.Index(vec.payload)
		require.NoError(t, err, fmt.Sprintf("test case %d", i))

		require.Len(t, entries, len(vec.expected))
		require.Equal(t, vec.expected, entries)
	}
}
