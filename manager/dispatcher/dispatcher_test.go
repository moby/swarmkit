package dispatcher

import (
	"net"
	"testing"
	"time"

	"golang.org/x/net/context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"

	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/manager/state"
	"github.com/stretchr/testify/assert"
)

type grpcDispatcher struct {
	Client           api.DispatcherClient
	Store            state.Store
	grpcServer       *grpc.Server
	dispatcherServer api.DispatcherServer
	conn             *grpc.ClientConn
}

func (gd *grpcDispatcher) Close() {
	// Close the client connection.
	_ = gd.conn.Close()
	gd.grpcServer.Stop()
}

func startDispatcher(c *Config) (*grpcDispatcher, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	s := grpc.NewServer()
	store := state.NewMemoryStore()
	d := New(store, c)
	api.RegisterDispatcherServer(s, d)
	go func() {
		// Serve will always return an error (even when properly stopped).
		// Explictly ignore it.
		_ = s.Serve(l)
	}()
	conn, err := grpc.Dial(l.Addr().String(), grpc.WithInsecure(), grpc.WithTimeout(10*time.Second))
	if err != nil {
		s.Stop()
		return nil, err
	}
	cli := api.NewDispatcherClient(conn)
	return &grpcDispatcher{
		Client:           cli,
		Store:            store,
		dispatcherServer: d,
		conn:             conn,
		grpcServer:       s,
	}, nil
}

func TestRegisterTwice(t *testing.T) {
	gd, err := startDispatcher(DefaultConfig())
	assert.NoError(t, err)
	defer gd.Close()

	testNode := &api.Node{ID: "test"}
	var expectedSessionID string
	{
		resp, err := gd.Client.Register(context.Background(), &api.RegisterRequest{NodeID: testNode.ID})
		assert.NoError(t, err)
		assert.Equal(t, resp.NodeID, testNode.ID)
		assert.NotEmpty(t, resp.SessionID)
		expectedSessionID = resp.SessionID
	}
	{
		resp, err := gd.Client.Register(context.Background(), &api.RegisterRequest{NodeID: testNode.ID})
		assert.NoError(t, err)
		assert.Equal(t, resp.NodeID, testNode.ID)
		// session should be different!
		assert.NotEqual(t, resp.SessionID, expectedSessionID)
	}
}

func TestHeartbeat(t *testing.T) {
	cfg := DefaultConfig()
	cfg.HeartbeatPeriod = 500 * time.Millisecond
	cfg.HeartbeatEpsilon = 0
	gd, err := startDispatcher(cfg)
	assert.NoError(t, err)
	defer gd.Close()

	testNode := &api.Node{ID: "test"}
	var expectedSessionID string
	{
		resp, err := gd.Client.Register(context.Background(), &api.RegisterRequest{NodeID: testNode.ID})
		assert.NoError(t, err)
		assert.Equal(t, resp.NodeID, testNode.ID)
		assert.NotEmpty(t, resp.SessionID)
		expectedSessionID = resp.SessionID
	}
	time.Sleep(250 * time.Millisecond)

	{
		// heartbeat without correct SessionID should fail
		resp, err := gd.Client.Heartbeat(context.Background(), &api.HeartbeatRequest{NodeID: testNode.ID})
		assert.Nil(t, resp)
		assert.Error(t, err)
		assert.Equal(t, grpc.Code(err), codes.InvalidArgument)
	}

	resp, err := gd.Client.Heartbeat(context.Background(), &api.HeartbeatRequest{NodeID: testNode.ID, SessionID: expectedSessionID})
	assert.NoError(t, err)
	assert.NotZero(t, resp.Period)
	time.Sleep(300 * time.Millisecond)

	err = gd.Store.View(func(readTx state.ReadTx) error {
		storeNode := readTx.Nodes().Get("test")
		assert.NotNil(t, storeNode)
		assert.Equal(t, storeNode.Status.State, api.NodeStatus_READY)
		return nil
	})
	assert.NoError(t, err)
}

func TestHeartbeatTimeout(t *testing.T) {
	cfg := DefaultConfig()
	cfg.HeartbeatPeriod = 100 * time.Millisecond
	cfg.HeartbeatEpsilon = 0
	gd, err := startDispatcher(cfg)
	assert.NoError(t, err)
	defer gd.Close()

	testNode := &api.Node{ID: "test"}
	var expectedSessionID string
	{
		resp, err := gd.Client.Register(context.Background(), &api.RegisterRequest{NodeID: testNode.ID})
		assert.NoError(t, err)
		assert.Equal(t, resp.NodeID, testNode.ID)
		assert.NotEmpty(t, resp.SessionID)
		expectedSessionID = resp.SessionID

	}
	time.Sleep(500 * time.Millisecond)

	err = gd.Store.View(func(readTx state.ReadTx) error {
		storeNode := readTx.Nodes().Get("test")
		assert.NotNil(t, storeNode)
		assert.Equal(t, api.NodeStatus_DOWN, storeNode.Status.State)
		return nil
	})
	assert.NoError(t, err)

	// check that node is deregistered
	resp, err := gd.Client.Heartbeat(context.Background(), &api.HeartbeatRequest{NodeID: testNode.ID, SessionID: expectedSessionID})
	assert.Nil(t, resp)
	assert.Error(t, err)
	assert.Equal(t, grpc.ErrorDesc(err), ErrNodeNotRegistered.Error())
}

func TestHeartbeatUnregistered(t *testing.T) {
	gd, err := startDispatcher(DefaultConfig())
	assert.NoError(t, err)
	defer gd.Close()
	resp, err := gd.Client.Heartbeat(context.Background(), &api.HeartbeatRequest{NodeID: "test"})
	assert.Nil(t, resp)
	assert.Error(t, err)
	assert.Equal(t, grpc.ErrorDesc(err), ErrNodeNotRegistered.Error())
}

func TestTasks(t *testing.T) {
	gd, err := startDispatcher(DefaultConfig())
	assert.NoError(t, err)
	defer gd.Close()
	testNode := &api.Node{ID: "test"}
	var expectedSessionID string
	{
		resp, err := gd.Client.Register(context.Background(), &api.RegisterRequest{NodeID: testNode.ID})
		assert.NoError(t, err)
		assert.Equal(t, resp.NodeID, testNode.ID)
		assert.NotEmpty(t, resp.SessionID)
		expectedSessionID = resp.SessionID
	}
	testTask1 := &api.Task{
		ID:     "testTask1",
		NodeID: testNode.ID,
	}
	testTask2 := &api.Task{
		ID:     "testTask2",
		NodeID: testNode.ID,
	}

	{
		// without correct SessionID should fail
		stream, err := gd.Client.Tasks(context.Background(), &api.TasksRequest{NodeID: testNode.ID})
		assert.NoError(t, err)
		assert.NotNil(t, stream)
		resp, err := stream.Recv()
		assert.Nil(t, resp)
		assert.Error(t, err)
		assert.Equal(t, grpc.Code(err), codes.InvalidArgument)
	}

	stream, err := gd.Client.Tasks(context.Background(), &api.TasksRequest{NodeID: testNode.ID, SessionID: expectedSessionID})
	assert.NoError(t, err)

	time.Sleep(100 * time.Millisecond)

	err = gd.Store.Update(func(tx state.Tx) error {
		assert.NoError(t, tx.Tasks().Create(testTask1))
		assert.NoError(t, tx.Tasks().Create(testTask2))
		return nil
	})
	assert.NoError(t, err)

	err = gd.Store.Update(func(tx state.Tx) error {
		assert.NoError(t, tx.Tasks().Update(&api.Task{
			ID:     testTask1.ID,
			NodeID: testNode.ID,
			Status: &api.TaskStatus{State: api.TaskStateFailed, Err: "1234"},
		}))
		return nil
	})
	assert.NoError(t, err)

	err = gd.Store.Update(func(tx state.Tx) error {
		assert.NoError(t, tx.Tasks().Delete(testTask1.ID))
		assert.NoError(t, tx.Tasks().Delete(testTask2.ID))
		return nil
	})
	assert.NoError(t, err)

	resp, err := stream.Recv()
	assert.NoError(t, err)
	assert.Equal(t, 0, len(resp.Tasks))

	resp, err = stream.Recv()
	assert.Equal(t, len(resp.Tasks), 1)
	assert.Equal(t, resp.Tasks[0].ID, "testTask1")

	resp, err = stream.Recv()
	assert.NoError(t, err)
	assert.Equal(t, len(resp.Tasks), 2)
	assert.True(t, resp.Tasks[0].ID == "testTask1" && resp.Tasks[1].ID == "testTask2" || resp.Tasks[0].ID == "testTask2" && resp.Tasks[1].ID == "testTask1")

	resp, err = stream.Recv()
	assert.NoError(t, err)
	assert.Equal(t, len(resp.Tasks), 2)
	for _, task := range resp.Tasks {
		if task.ID == "testTask1" {
			assert.Equal(t, task.Status.State, api.TaskStateFailed)
			assert.Equal(t, task.Status.Err, "1234")
		}
	}

	resp, err = stream.Recv()
	assert.NoError(t, err)
	assert.Equal(t, len(resp.Tasks), 1)

	resp, err = stream.Recv()
	assert.NoError(t, err)
	assert.Equal(t, len(resp.Tasks), 0)
}

func TestTaskUpdate(t *testing.T) {
	gd, err := startDispatcher(DefaultConfig())
	assert.NoError(t, err)
	defer gd.Close()

	testNode := &api.Node{ID: "test"}
	var expectedSessionID string
	{
		resp, err := gd.Client.Register(context.Background(), &api.RegisterRequest{NodeID: testNode.ID})
		assert.NoError(t, err)
		assert.Equal(t, resp.NodeID, testNode.ID)
		assert.NotEmpty(t, resp.SessionID)
		expectedSessionID = resp.SessionID
	}
	testTask1 := &api.Task{
		ID:     "testTask1",
		NodeID: testNode.ID,
	}
	testTask2 := &api.Task{
		ID:     "testTask2",
		NodeID: testNode.ID,
	}
	err = gd.Store.Update(func(tx state.Tx) error {
		assert.NoError(t, tx.Tasks().Create(testTask1))
		assert.NoError(t, tx.Tasks().Create(testTask2))
		return nil
	})
	assert.NoError(t, err)

	testTask1.Status = &api.TaskStatus{State: api.TaskStateAssigned}
	testTask2.Status = &api.TaskStatus{State: api.TaskStateAssigned}
	updReq := &api.UpdateTaskStatusRequest{
		NodeID: testNode.ID,
		Tasks:  []*api.Task{testTask1, testTask2},
	}

	{
		// without correct SessionID should fail
		resp, err := gd.Client.UpdateTaskStatus(context.Background(), updReq)
		assert.Nil(t, resp)
		assert.Error(t, err)
		assert.Equal(t, grpc.Code(err), codes.InvalidArgument)
	}

	updReq.SessionID = expectedSessionID
	_, err = gd.Client.UpdateTaskStatus(context.Background(), updReq)
	assert.NoError(t, err)
	err = gd.Store.View(func(readTx state.ReadTx) error {
		storeTask1 := readTx.Tasks().Get(testTask1.ID)
		assert.NotNil(t, storeTask1)
		storeTask2 := readTx.Tasks().Get(testTask2.ID)
		assert.NotNil(t, storeTask2)
		assert.Equal(t, storeTask1.Status.State, api.TaskStateAssigned)
		assert.Equal(t, storeTask2.Status.State, api.TaskStateAssigned)
		return nil
	})
	assert.NoError(t, err)
}

func TestSession(t *testing.T) {
	cfg := DefaultConfig()
	gd, err := startDispatcher(cfg)
	assert.NoError(t, err)
	defer gd.Close()

	testNode := &api.Node{ID: "test"}
	resp, err := gd.Client.Register(context.Background(), &api.RegisterRequest{NodeID: testNode.ID})
	assert.NoError(t, err)
	assert.Equal(t, resp.NodeID, testNode.ID)
	assert.NotEmpty(t, resp.SessionID)
	sid := resp.SessionID

	stream, err := gd.Client.Session(context.Background(), &api.SessionRequest{NodeID: testNode.ID, SessionID: sid})
	msg, err := stream.Recv()
	assert.Equal(t, 1, len(msg.Managers))
	assert.False(t, msg.Disconnect)
}
