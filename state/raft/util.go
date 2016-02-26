package raft

import (
	"errors"
	"time"

	"github.com/docker/swarm-v2/state/raft/pb"
	"github.com/gogo/protobuf/proto"

	"google.golang.org/grpc"
)

const (
	// MaxRetryTime is the number of time we try to initiate
	// a grpc connection to a remote raft member
	MaxRetryTime = 3
)

// Raft represents a connection to a raft member
type Raft struct {
	pb.RaftClient
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
		RaftClient: pb.NewRaftClient(conn),
		Conn:       conn,
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
	pair := &pb.Pair{
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
	pb.RegisterRaftServer(server, node)
}
