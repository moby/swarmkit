package dispatcher

import (
	"net"
	"os"
	"testing"
	"time"

	"golang.org/x/net/context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"

	"github.com/Sirupsen/logrus"
	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/ca"
	"github.com/docker/swarm-v2/ca/testutils"
	"github.com/docker/swarm-v2/manager/state/store"
	"github.com/stretchr/testify/assert"
)

type grpcDispatcher struct {
	Clients              []api.DispatcherClient
	Store                *store.MemoryStore
	grpcServer           *grpc.Server
	dispatcherServer     *Dispatcher
	conns                []*grpc.ClientConn
	agentSecurityConfigs []*ca.AgentSecurityConfig
}

func (gd *grpcDispatcher) Close() {
	// Close the client connection.
	gd.dispatcherServer.Stop()
	for _, conn := range gd.conns {
		conn.Close()
	}
	gd.grpcServer.Stop()
}

type testCluster struct {
	addr  string
	store *store.MemoryStore
}

func (t *testCluster) GetMemberlist() map[uint64]*api.Member {
	return map[uint64]*api.Member{
		1: {
			Addr: t.addr,
		},
	}
}

func (t *testCluster) MemoryStore() *store.MemoryStore {
	return t.store
}

func startDispatcher(c *Config) (*grpcDispatcher, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}

	agentSecurityConfigs, managerSecurityConfig, tmpDir, err := testutils.GenerateAgentAndManagerSecurityConfig(2)
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmpDir)

	serverOpts := []grpc.ServerOption{grpc.Creds(managerSecurityConfig.ServerTLSCreds)}

	s := grpc.NewServer(serverOpts...)
	tc := &testCluster{addr: l.Addr().String(), store: store.NewMemoryStore(nil)}
	d := New(tc, c)
	api.RegisterDispatcherServer(s, d)
	go func() {
		// Serve will always return an error (even when properly stopped).
		// Explicitly ignore it.
		_ = s.Serve(l)
	}()
	go d.Run(context.Background())

	clientOpts := []grpc.DialOption{grpc.WithTimeout(10 * time.Second)}
	clientOpts1 := append(clientOpts, grpc.WithTransportCredentials(agentSecurityConfigs[0].ClientTLSCreds))
	clientOpts2 := append(clientOpts, grpc.WithTransportCredentials(agentSecurityConfigs[1].ClientTLSCreds))

	conn1, err := grpc.Dial(l.Addr().String(), clientOpts1...)
	if err != nil {
		s.Stop()
		return nil, err
	}

	conn2, err := grpc.Dial(l.Addr().String(), clientOpts2...)
	if err != nil {
		s.Stop()
		return nil, err
	}

	clients := []api.DispatcherClient{api.NewDispatcherClient(conn1), api.NewDispatcherClient(conn2)}
	conns := []*grpc.ClientConn{conn1, conn2}
	return &grpcDispatcher{
		Clients:              clients,
		Store:                tc.MemoryStore(),
		dispatcherServer:     d,
		conns:                conns,
		grpcServer:           s,
		agentSecurityConfigs: agentSecurityConfigs,
	}, nil
}

func TestRegisterTwice(t *testing.T) {
	gd, err := startDispatcher(DefaultConfig())
	assert.NoError(t, err)
	defer gd.Close()

	var expectedSessionID string
	{
		resp, err := gd.Clients[0].Register(context.Background(), &api.RegisterRequest{})
		assert.NoError(t, err)
		assert.NotEmpty(t, resp.SessionID)
		expectedSessionID = resp.SessionID
	}
	{
		resp, err := gd.Clients[0].Register(context.Background(), &api.RegisterRequest{})
		assert.NoError(t, err)
		// session should be different!
		assert.NotEqual(t, resp.SessionID, expectedSessionID)
	}
}

func TestHeartbeat(t *testing.T) {
	cfg := DefaultConfig()
	cfg.HeartbeatPeriod = 500 * time.Millisecond
	cfg.HeartbeatEpsilon = 0
	gd, err := startDispatcher(DefaultConfig())
	assert.NoError(t, err)
	defer gd.Close()

	var expectedSessionID string
	{
		resp, err := gd.Clients[0].Register(context.Background(), &api.RegisterRequest{})
		assert.NoError(t, err)
		assert.NotEmpty(t, resp.SessionID)
		expectedSessionID = resp.SessionID
	}
	time.Sleep(250 * time.Millisecond)

	{
		// heartbeat without correct SessionID should fail
		resp, err := gd.Clients[0].Heartbeat(context.Background(), &api.HeartbeatRequest{})
		assert.Nil(t, resp)
		assert.Error(t, err)
		assert.Equal(t, grpc.Code(err), codes.InvalidArgument)
	}

	resp, err := gd.Clients[0].Heartbeat(context.Background(), &api.HeartbeatRequest{SessionID: expectedSessionID})
	assert.NoError(t, err)
	assert.NotZero(t, resp.Period)
	time.Sleep(300 * time.Millisecond)

	gd.Store.View(func(readTx store.ReadTx) {
		storeNodes, err := store.FindNodes(readTx, store.All)
		assert.NoError(t, err)
		assert.NotEmpty(t, storeNodes)
		assert.Equal(t, storeNodes[0].Status.State, api.NodeStatus_READY)
	})
}

func TestHeartbeatTimeout(t *testing.T) {
	logrus.SetLevel(logrus.DebugLevel)
	cfg := DefaultConfig()
	cfg.HeartbeatPeriod = 100 * time.Millisecond
	cfg.HeartbeatEpsilon = 0
	gd, err := startDispatcher(cfg)
	assert.NoError(t, err)
	defer gd.Close()

	var expectedSessionID string
	{
		resp, err := gd.Clients[0].Register(context.Background(), &api.RegisterRequest{})
		assert.NoError(t, err)
		assert.NotEmpty(t, resp.SessionID)
		expectedSessionID = resp.SessionID

	}
	time.Sleep(500 * time.Millisecond)

	gd.Store.View(func(readTx store.ReadTx) {
		storeNodes, err := store.FindNodes(readTx, store.All)
		assert.NoError(t, err)
		assert.NotEmpty(t, storeNodes)
		assert.Equal(t, api.NodeStatus_DOWN, storeNodes[0].Status.State)
	})

	// check that node is deregistered
	resp, err := gd.Clients[0].Heartbeat(context.Background(), &api.HeartbeatRequest{SessionID: expectedSessionID})
	assert.Nil(t, resp)
	assert.Error(t, err)
	assert.Equal(t, grpc.ErrorDesc(err), ErrNodeNotRegistered.Error())
}

func TestHeartbeatUnregistered(t *testing.T) {
	gd, err := startDispatcher(DefaultConfig())
	assert.NoError(t, err)
	defer gd.Close()
	resp, err := gd.Clients[0].Heartbeat(context.Background(), &api.HeartbeatRequest{})
	assert.Nil(t, resp)
	assert.Error(t, err)
	assert.Equal(t, grpc.ErrorDesc(err), ErrNodeNotRegistered.Error())
}

func TestTasks(t *testing.T) {
	gd, err := startDispatcher(DefaultConfig())
	assert.NoError(t, err)
	defer gd.Close()

	var expectedSessionID string
	var nodeID string
	{
		resp, err := gd.Clients[0].Register(context.Background(), &api.RegisterRequest{})
		assert.NoError(t, err)
		assert.NotEmpty(t, resp.SessionID)
		expectedSessionID = resp.SessionID
		nodeID = resp.NodeID
	}

	testTask1 := &api.Task{
		NodeID: nodeID,
		ID:     "testTask1",
		Status: api.TaskStatus{State: api.TaskStateAssigned},
	}
	testTask2 := &api.Task{
		NodeID: nodeID,
		ID:     "testTask2",
		Status: api.TaskStatus{State: api.TaskStateAssigned},
	}

	{
		// without correct SessionID should fail
		stream, err := gd.Clients[0].Tasks(context.Background(), &api.TasksRequest{})
		assert.NoError(t, err)
		assert.NotNil(t, stream)
		resp, err := stream.Recv()
		assert.Nil(t, resp)
		assert.Error(t, err)
		assert.Equal(t, grpc.Code(err), codes.InvalidArgument)
	}

	stream, err := gd.Clients[0].Tasks(context.Background(), &api.TasksRequest{SessionID: expectedSessionID})
	assert.NoError(t, err)

	time.Sleep(100 * time.Millisecond)

	err = gd.Store.Update(func(tx store.Tx) error {
		assert.NoError(t, store.CreateTask(tx, testTask1))
		assert.NoError(t, store.CreateTask(tx, testTask2))
		return nil
	})
	assert.NoError(t, err)

	err = gd.Store.Update(func(tx store.Tx) error {
		assert.NoError(t, store.UpdateTask(tx, &api.Task{
			ID:     testTask1.ID,
			NodeID: nodeID,
			Status: api.TaskStatus{State: api.TaskStateFailed, Err: "1234"},
		}))
		return nil
	})
	assert.NoError(t, err)

	err = gd.Store.Update(func(tx store.Tx) error {
		assert.NoError(t, store.DeleteTask(tx, testTask1.ID))
		assert.NoError(t, store.DeleteTask(tx, testTask2.ID))
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

	var expectedSessionID string
	{
		resp, err := gd.Clients[0].Register(context.Background(), &api.RegisterRequest{})
		assert.NoError(t, err)
		assert.NotEmpty(t, resp.SessionID)
		expectedSessionID = resp.SessionID
	}
	testTask1 := &api.Task{
		ID: "testTask1",
	}
	testTask2 := &api.Task{
		ID: "testTask2",
	}
	err = gd.Store.Update(func(tx store.Tx) error {
		assert.NoError(t, store.CreateTask(tx, testTask1))
		assert.NoError(t, store.CreateTask(tx, testTask2))
		return nil
	})
	assert.NoError(t, err)

	testTask1.Status = api.TaskStatus{State: api.TaskStateAssigned}
	testTask2.Status = api.TaskStatus{State: api.TaskStateAssigned}
	updReq := &api.UpdateTaskStatusRequest{
		Updates: []*api.UpdateTaskStatusRequest_TaskStatusUpdate{
			{
				TaskID: testTask1.ID,
				Status: &testTask1.Status,
			},
			{
				TaskID: testTask2.ID,
				Status: &testTask2.Status,
			},
		},
	}
	{
		// without correct SessionID should fail
		resp, err := gd.Clients[0].UpdateTaskStatus(context.Background(), updReq)
		assert.Nil(t, resp)
		assert.Error(t, err)
		assert.Equal(t, grpc.Code(err), codes.InvalidArgument)
	}

	updReq.SessionID = expectedSessionID
	_, err = gd.Clients[0].UpdateTaskStatus(context.Background(), updReq)
	assert.NoError(t, err)
	gd.Store.View(func(readTx store.ReadTx) {
		storeTask1 := store.GetTask(readTx, testTask1.ID)
		assert.NotNil(t, storeTask1)
		assert.NotNil(t, storeTask1.Status)
		storeTask2 := store.GetTask(readTx, testTask2.ID)
		assert.NotNil(t, storeTask2)
		assert.NotNil(t, storeTask2.Status)
		assert.Equal(t, storeTask1.Status.State, api.TaskStateAssigned)
		assert.Equal(t, storeTask2.Status.State, api.TaskStateAssigned)
	})
}

func TestSession(t *testing.T) {
	cfg := DefaultConfig()
	gd, err := startDispatcher(cfg)
	assert.NoError(t, err)
	defer gd.Close()

	resp, err := gd.Clients[0].Register(context.Background(), &api.RegisterRequest{})
	assert.NoError(t, err)
	assert.NotEmpty(t, resp.SessionID)
	sid := resp.SessionID

	stream, err := gd.Clients[0].Session(context.Background(), &api.SessionRequest{SessionID: sid})
	msg, err := stream.Recv()
	assert.Equal(t, 1, len(msg.Managers))
	assert.False(t, msg.Disconnect)
}

func TestNodesCount(t *testing.T) {
	cfg := DefaultConfig()
	cfg.HeartbeatPeriod = 100 * time.Millisecond
	cfg.HeartbeatEpsilon = 0
	gd, err := startDispatcher(cfg)
	assert.NoError(t, err)
	defer gd.Close()

	{
		_, err := gd.Clients[0].Register(context.Background(), &api.RegisterRequest{})
		assert.NoError(t, err)
	}
	{
		_, err := gd.Clients[1].Register(context.Background(), &api.RegisterRequest{})
		assert.NoError(t, err)
	}
	assert.Equal(t, 2, gd.dispatcherServer.NodeCount())
	time.Sleep(500 * time.Millisecond)
	assert.Equal(t, 0, gd.dispatcherServer.NodeCount())
}
