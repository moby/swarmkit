package state

import (
	"errors"
	"time"

	"github.com/docker/swarm-v2/api"
	"github.com/gogo/protobuf/proto"

	"google.golang.org/grpc"
)

const (
	// MaxRetries is the maximum number of retries allowed to
	// initiate a grpc connection to a remote raft member
	MaxRetries = 3
)

// Raft represents a connection to a raft member
type Raft struct {
	api.ManagerClient
	Conn *grpc.ClientConn
}

// GetRaftClient returns a raft client object to communicate
// with other raft members
func GetRaftClient(addr string, timeout time.Duration) (*Raft, error) {
	conn, err := dial(addr, "tcp", timeout)
	if err != nil {
		return nil, err
	}

	return &Raft{
		ManagerClient: api.NewManagerClient(conn),
		Conn:          conn,
	}, nil
}

// dial returns a grpc client connection
func dial(addr string, protocol string, timeout time.Duration) (*grpc.ClientConn, error) {
	conn, err := grpc.Dial(addr, grpc.WithInsecure(), grpc.WithTimeout(timeout))
	if err != nil {
		return nil, err
	}

	return conn, nil
}

// EncodePair returns a protobuf encoded key/value pair to be sent through raft
func EncodePair(key string, value []byte) ([]byte, error) {
	k := proto.String(key)
	pair := &api.Pair{
		Key:   *k,
		Value: value,
	}
	data, err := proto.Marshal(pair)
	if err != nil {
		return nil, errors.New("Can't encode key/value using protobuf")
	}
	return data, nil
}

// Register registers the node raft server
func Register(server *grpc.Server, node *Node) {
	api.RegisterManagerServer(server, node)
}
