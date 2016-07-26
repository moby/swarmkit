package dispatcher

import (
	"crypto/tls"
	"fmt"
	"net"
	"testing"
	"time"

	"golang.org/x/net/context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"

	"github.com/docker/swarmkit/api"
	"github.com/docker/swarmkit/ca"
	"github.com/docker/swarmkit/ca/testutils"
	raftutils "github.com/docker/swarmkit/manager/state/raft/testutils"
	"github.com/docker/swarmkit/manager/state/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type grpcDispatcher struct {
	Clients          []api.DispatcherClient
	SecurityConfigs  []*ca.SecurityConfig
	Store            *store.MemoryStore
	grpcServer       *grpc.Server
	dispatcherServer *Dispatcher
	conns            []*grpc.ClientConn
	testCA           *testutils.TestCA
}

func (gd *grpcDispatcher) Close() {
	// Close the client connection.
	gd.dispatcherServer.Stop()
	for _, conn := range gd.conns {
		conn.Close()
	}
	gd.grpcServer.Stop()
	gd.testCA.Stop()
}

type testCluster struct {
	addr  string
	store *store.MemoryStore
}

func (t *testCluster) GetMemberlist() map[uint64]*api.RaftMember {
	return map[uint64]*api.RaftMember{
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

	tca := testutils.NewTestCA(nil)
	agentSecurityConfig1, err := tca.NewNodeConfig(ca.AgentRole)
	if err != nil {
		return nil, err
	}
	agentSecurityConfig2, err := tca.NewNodeConfig(ca.AgentRole)
	if err != nil {
		return nil, err
	}
	managerSecurityConfig, err := tca.NewNodeConfig(ca.ManagerRole)
	if err != nil {
		return nil, err
	}

	serverOpts := []grpc.ServerOption{grpc.Creds(managerSecurityConfig.ServerTLSCreds)}

	s := grpc.NewServer(serverOpts...)
	tc := &testCluster{addr: l.Addr().String(), store: tca.MemoryStore}
	d := New(tc, c)

	authorize := func(ctx context.Context, roles []string) error {
		_, err := ca.AuthorizeForwardedRoleAndOrg(ctx, roles, []string{ca.ManagerRole}, tca.Organization)
		return err
	}
	authenticatedDispatcherAPI := api.NewAuthenticatedWrapperDispatcherServer(d, authorize)

	api.RegisterDispatcherServer(s, authenticatedDispatcherAPI)
	go func() {
		// Serve will always return an error (even when properly stopped).
		// Explicitly ignore it.
		_ = s.Serve(l)
	}()
	go d.Run(context.Background())
	if err := raftutils.PollFuncWithTimeout(nil, func() error {
		d.mu.Lock()
		defer d.mu.Unlock()
		if !d.isRunning() {
			return fmt.Errorf("dispatcher is not running")
		}
		return nil
	}, 5*time.Second); err != nil {
		return nil, err
	}

	clientOpts := []grpc.DialOption{grpc.WithTimeout(10 * time.Second)}
	clientOpts1 := append(clientOpts, grpc.WithTransportCredentials(agentSecurityConfig1.ClientTLSCreds))
	clientOpts2 := append(clientOpts, grpc.WithTransportCredentials(agentSecurityConfig2.ClientTLSCreds))
	clientOpts3 := append(clientOpts, grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{InsecureSkipVerify: true})))

	conn1, err := grpc.Dial(l.Addr().String(), clientOpts1...)
	if err != nil {
		return nil, err
	}

	conn2, err := grpc.Dial(l.Addr().String(), clientOpts2...)
	if err != nil {
		return nil, err
	}

	conn3, err := grpc.Dial(l.Addr().String(), clientOpts3...)
	if err != nil {
		return nil, err
	}

	clients := []api.DispatcherClient{api.NewDispatcherClient(conn1), api.NewDispatcherClient(conn2), api.NewDispatcherClient(conn3)}
	securityConfigs := []*ca.SecurityConfig{agentSecurityConfig1, agentSecurityConfig2, managerSecurityConfig}
	conns := []*grpc.ClientConn{conn1, conn2, conn3}
	return &grpcDispatcher{
		Clients:          clients,
		SecurityConfigs:  securityConfigs,
		Store:            tc.MemoryStore(),
		dispatcherServer: d,
		conns:            conns,
		grpcServer:       s,
		testCA:           tca,
	}, nil
}

func TestRegisterTwice(t *testing.T) {
	cfg := DefaultConfig()
	cfg.RateLimitPeriod = 0
	gd, err := startDispatcher(cfg)
	assert.NoError(t, err)
	defer gd.Close()

	var expectedSessionID string
	{
		stream, err := gd.Clients[0].Session(context.Background(), &api.SessionRequest{})
		assert.NoError(t, err)
		msg, err := stream.Recv()
		assert.NoError(t, err)
		assert.NotEmpty(t, msg.SessionID)
		expectedSessionID = msg.SessionID
		stream.CloseSend()
	}
	{
		stream, err := gd.Clients[0].Session(context.Background(), &api.SessionRequest{})
		assert.NoError(t, err)
		msg, err := stream.Recv()

		assert.NoError(t, err)
		// session should be different!
		assert.NotEqual(t, msg.SessionID, expectedSessionID)
		stream.CloseSend()
	}
}

func TestRegisterExceedRateLimit(t *testing.T) {
	gd, err := startDispatcher(DefaultConfig())
	assert.NoError(t, err)
	defer gd.Close()

	for i := 0; i < 3; i++ {
		stream, err := gd.Clients[0].Session(context.Background(), &api.SessionRequest{})
		assert.NoError(t, err)
		msg, err := stream.Recv()
		assert.NoError(t, err)
		assert.NotEmpty(t, msg.SessionID)
		stream.CloseSend()
	}
	{
		stream, err := gd.Clients[0].Session(context.Background(), &api.SessionRequest{})
		defer stream.CloseSend()
		assert.NoError(t, err)
		_, err = stream.Recv()
		assert.Error(t, err)
		assert.Equal(t, codes.Unavailable, grpc.Code(err))
	}
}

func TestRegisterNoCert(t *testing.T) {
	gd, err := startDispatcher(DefaultConfig())
	assert.NoError(t, err)
	defer gd.Close()

	// This client has no certificates, this should fail
	stream, err := gd.Clients[2].Session(context.Background(), &api.SessionRequest{})
	assert.NoError(t, err)
	defer stream.CloseSend()
	resp, err := stream.Recv()
	assert.Nil(t, resp)
	assert.EqualError(t, err, "rpc error: code = 7 desc = Permission denied: unauthorized peer role: rpc error: code = 7 desc = no client certificates in request")
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
		stream, err := gd.Clients[0].Session(context.Background(), &api.SessionRequest{})
		assert.NoError(t, err)
		defer stream.CloseSend()

		resp, err := stream.Recv()
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
		found := false
		for _, node := range storeNodes {
			if node.ID == gd.SecurityConfigs[0].ClientTLSCreds.NodeID() {
				found = true
				assert.Equal(t, api.NodeStatus_READY, node.Status.State)
			}
		}
		assert.True(t, found)
	})
}

func TestHeartbeatNoCert(t *testing.T) {
	gd, err := startDispatcher(DefaultConfig())
	assert.NoError(t, err)
	defer gd.Close()

	// heartbeat without correct SessionID should fail
	resp, err := gd.Clients[2].Heartbeat(context.Background(), &api.HeartbeatRequest{})
	assert.Nil(t, resp)
	assert.EqualError(t, err, "rpc error: code = 7 desc = Permission denied: unauthorized peer role: rpc error: code = 7 desc = no client certificates in request")
}

func TestHeartbeatTimeout(t *testing.T) {
	cfg := DefaultConfig()
	cfg.HeartbeatPeriod = 100 * time.Millisecond
	cfg.HeartbeatEpsilon = 0
	gd, err := startDispatcher(cfg)
	assert.NoError(t, err)
	defer gd.Close()

	var expectedSessionID string
	{
		stream, err := gd.Clients[0].Session(context.Background(), &api.SessionRequest{})
		assert.NoError(t, err)
		resp, err := stream.Recv()
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
	assert.Equal(t, ErrSessionInvalid.Error(), grpc.ErrorDesc(err))
}

func TestTasks(t *testing.T) {
	gd, err := startDispatcher(DefaultConfig())
	assert.NoError(t, err)
	defer gd.Close()

	var expectedSessionID string
	var nodeID string
	{
		stream, err := gd.Clients[0].Session(context.Background(), &api.SessionRequest{})
		assert.NoError(t, err)
		defer stream.CloseSend()
		resp, err := stream.Recv()
		assert.NoError(t, err)
		assert.NotEmpty(t, resp.SessionID)
		expectedSessionID = resp.SessionID
		nodeID = resp.Node.ID
	}

	testTask1 := &api.Task{
		NodeID:       nodeID,
		ID:           "testTask1",
		Status:       api.TaskStatus{State: api.TaskStateAssigned},
		DesiredState: api.TaskStateReady,
	}
	testTask2 := &api.Task{
		NodeID:       nodeID,
		ID:           "testTask2",
		Status:       api.TaskStatus{State: api.TaskStateAssigned},
		DesiredState: api.TaskStateReady,
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

	resp, err := stream.Recv()
	assert.NoError(t, err)
	// initially no tasks
	assert.Equal(t, 0, len(resp.Tasks))

	err = gd.Store.Update(func(tx store.Tx) error {
		assert.NoError(t, store.CreateTask(tx, testTask1))
		assert.NoError(t, store.CreateTask(tx, testTask2))
		return nil
	})
	assert.NoError(t, err)

	resp, err = stream.Recv()
	assert.NoError(t, err)
	assert.Equal(t, len(resp.Tasks), 2)
	assert.True(t, resp.Tasks[0].ID == "testTask1" && resp.Tasks[1].ID == "testTask2" || resp.Tasks[0].ID == "testTask2" && resp.Tasks[1].ID == "testTask1")

	err = gd.Store.Update(func(tx store.Tx) error {
		assert.NoError(t, store.UpdateTask(tx, &api.Task{
			ID:           testTask1.ID,
			NodeID:       nodeID,
			Status:       api.TaskStatus{State: api.TaskStateAssigned},
			DesiredState: api.TaskStateRunning,
		}))
		return nil
	})
	assert.NoError(t, err)

	resp, err = stream.Recv()
	assert.NoError(t, err)
	assert.Equal(t, len(resp.Tasks), 2)
	for _, task := range resp.Tasks {
		if task.ID == "testTask1" {
			assert.Equal(t, task.DesiredState, api.TaskStateRunning)
		}
	}

	err = gd.Store.Update(func(tx store.Tx) error {
		assert.NoError(t, store.DeleteTask(tx, testTask1.ID))
		assert.NoError(t, store.DeleteTask(tx, testTask2.ID))
		return nil
	})
	assert.NoError(t, err)

	resp, err = stream.Recv()
	assert.NoError(t, err)
	assert.Equal(t, len(resp.Tasks), 0)
}

func TestTasksStatusChange(t *testing.T) {
	gd, err := startDispatcher(DefaultConfig())
	assert.NoError(t, err)
	defer gd.Close()

	var expectedSessionID string
	var nodeID string
	{
		stream, err := gd.Clients[0].Session(context.Background(), &api.SessionRequest{})
		assert.NoError(t, err)
		defer stream.CloseSend()
		resp, err := stream.Recv()
		assert.NoError(t, err)
		assert.NotEmpty(t, resp.SessionID)
		expectedSessionID = resp.SessionID
		nodeID = resp.Node.ID
	}

	testTask1 := &api.Task{
		NodeID:       nodeID,
		ID:           "testTask1",
		Status:       api.TaskStatus{State: api.TaskStateAssigned},
		DesiredState: api.TaskStateReady,
	}
	testTask2 := &api.Task{
		NodeID:       nodeID,
		ID:           "testTask2",
		Status:       api.TaskStatus{State: api.TaskStateAssigned},
		DesiredState: api.TaskStateReady,
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

	resp, err := stream.Recv()
	assert.NoError(t, err)
	// initially no tasks
	assert.Equal(t, 0, len(resp.Tasks))

	err = gd.Store.Update(func(tx store.Tx) error {
		assert.NoError(t, store.CreateTask(tx, testTask1))
		assert.NoError(t, store.CreateTask(tx, testTask2))
		return nil
	})
	assert.NoError(t, err)

	resp, err = stream.Recv()
	assert.NoError(t, err)
	assert.Equal(t, len(resp.Tasks), 2)
	assert.True(t, resp.Tasks[0].ID == "testTask1" && resp.Tasks[1].ID == "testTask2" || resp.Tasks[0].ID == "testTask2" && resp.Tasks[1].ID == "testTask1")

	err = gd.Store.Update(func(tx store.Tx) error {
		assert.NoError(t, store.UpdateTask(tx, &api.Task{
			ID:     testTask1.ID,
			NodeID: nodeID,
			// only Status is changed for task1
			Status:       api.TaskStatus{State: api.TaskStateFailed, Err: "1234"},
			DesiredState: api.TaskStateReady,
		}))
		return nil
	})
	assert.NoError(t, err)

	// dispatcher shouldn't send snapshot for this update
	recvChan := make(chan struct{})
	go func() {
		_, _ = stream.Recv()
		recvChan <- struct{}{}
	}()

	select {
	case <-recvChan:
		assert.Fail(t, "task.Status update should not trigger dispatcher update")
	case <-time.After(250 * time.Millisecond):
	}
}

func TestTasksBatch(t *testing.T) {
	gd, err := startDispatcher(DefaultConfig())
	assert.NoError(t, err)
	defer gd.Close()

	var expectedSessionID string
	var nodeID string
	{
		stream, err := gd.Clients[0].Session(context.Background(), &api.SessionRequest{})
		assert.NoError(t, err)
		defer stream.CloseSend()
		resp, err := stream.Recv()
		assert.NoError(t, err)
		assert.NotEmpty(t, resp.SessionID)
		expectedSessionID = resp.SessionID
		nodeID = resp.Node.ID
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

	stream, err := gd.Clients[0].Tasks(context.Background(), &api.TasksRequest{SessionID: expectedSessionID})
	assert.NoError(t, err)

	resp, err := stream.Recv()
	assert.NoError(t, err)
	// initially no tasks
	assert.Equal(t, 0, len(resp.Tasks))

	err = gd.Store.Update(func(tx store.Tx) error {
		assert.NoError(t, store.CreateTask(tx, testTask1))
		assert.NoError(t, store.CreateTask(tx, testTask2))
		return nil
	})
	assert.NoError(t, err)

	err = gd.Store.Update(func(tx store.Tx) error {
		assert.NoError(t, store.DeleteTask(tx, testTask1.ID))
		assert.NoError(t, store.DeleteTask(tx, testTask2.ID))
		return nil
	})
	assert.NoError(t, err)

	resp, err = stream.Recv()
	assert.NoError(t, err)
	// all tasks have been deleted
	assert.Equal(t, len(resp.Tasks), 0)
}

func TestTasksNoCert(t *testing.T) {
	gd, err := startDispatcher(DefaultConfig())
	assert.NoError(t, err)
	defer gd.Close()

	stream, err := gd.Clients[2].Tasks(context.Background(), &api.TasksRequest{})
	assert.NoError(t, err)
	assert.NotNil(t, stream)
	resp, err := stream.Recv()
	assert.Nil(t, resp)
	assert.EqualError(t, err, "rpc error: code = 7 desc = Permission denied: unauthorized peer role: rpc error: code = 7 desc = no client certificates in request")
}

func TestTaskUpdate(t *testing.T) {
	gd, err := startDispatcher(DefaultConfig())
	assert.NoError(t, err)
	defer gd.Close()

	var (
		expectedSessionID string
		nodeID            string
	)
	{
		stream, err := gd.Clients[0].Session(context.Background(), &api.SessionRequest{})
		assert.NoError(t, err)
		defer stream.CloseSend()
		resp, err := stream.Recv()
		assert.NoError(t, err)
		assert.NotEmpty(t, resp.SessionID)
		expectedSessionID = resp.SessionID
		nodeID = resp.Node.ID

	}
	testTask1 := &api.Task{
		ID:     "testTask1",
		NodeID: nodeID,
	}
	testTask2 := &api.Task{
		ID:     "testTask2",
		NodeID: nodeID,
	}
	testTask3 := &api.Task{
		ID:     "testTask3",
		NodeID: "differentnode",
	}
	err = gd.Store.Update(func(tx store.Tx) error {
		assert.NoError(t, store.CreateTask(tx, testTask1))
		assert.NoError(t, store.CreateTask(tx, testTask2))
		assert.NoError(t, store.CreateTask(tx, testTask3))
		return nil
	})
	assert.NoError(t, err)

	testTask1.Status = api.TaskStatus{State: api.TaskStateAssigned}
	testTask2.Status = api.TaskStatus{State: api.TaskStateAssigned}
	testTask3.Status = api.TaskStatus{State: api.TaskStateAssigned}
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

	{
		// updating a task not assigned to us should fail
		updReq.Updates = []*api.UpdateTaskStatusRequest_TaskStatusUpdate{
			{
				TaskID: testTask3.ID,
				Status: &testTask3.Status,
			},
		}

		resp, err := gd.Clients[0].UpdateTaskStatus(context.Background(), updReq)
		assert.Nil(t, resp)
		assert.Error(t, err)
		assert.Equal(t, grpc.Code(err), codes.PermissionDenied)
	}

	gd.dispatcherServer.processTaskUpdates()

	gd.Store.View(func(readTx store.ReadTx) {
		storeTask1 := store.GetTask(readTx, testTask1.ID)
		assert.NotNil(t, storeTask1)
		assert.NotNil(t, storeTask1.Status)
		storeTask2 := store.GetTask(readTx, testTask2.ID)
		assert.NotNil(t, storeTask2)
		assert.NotNil(t, storeTask2.Status)
		assert.Equal(t, storeTask1.Status.State, api.TaskStateAssigned)
		assert.Equal(t, storeTask2.Status.State, api.TaskStateAssigned)

		storeTask3 := store.GetTask(readTx, testTask3.ID)
		assert.NotNil(t, storeTask3)
		assert.NotNil(t, storeTask3.Status)
		assert.Equal(t, storeTask3.Status.State, api.TaskStateNew)

	})

}

func TestTaskUpdateNoCert(t *testing.T) {
	gd, err := startDispatcher(DefaultConfig())
	assert.NoError(t, err)
	defer gd.Close()

	testTask1 := &api.Task{
		ID: "testTask1",
	}
	err = gd.Store.Update(func(tx store.Tx) error {
		assert.NoError(t, store.CreateTask(tx, testTask1))
		return nil
	})
	assert.NoError(t, err)

	testTask1.Status = api.TaskStatus{State: api.TaskStateAssigned}
	updReq := &api.UpdateTaskStatusRequest{
		Updates: []*api.UpdateTaskStatusRequest_TaskStatusUpdate{
			{
				TaskID: testTask1.ID,
				Status: &testTask1.Status,
			},
		},
	}
	// without correct SessionID should fail
	resp, err := gd.Clients[2].UpdateTaskStatus(context.Background(), updReq)
	assert.Nil(t, resp)
	assert.Error(t, err)
	assert.EqualError(t, err, "rpc error: code = 7 desc = Permission denied: unauthorized peer role: rpc error: code = 7 desc = no client certificates in request")
}

func TestSession(t *testing.T) {
	cfg := DefaultConfig()
	gd, err := startDispatcher(cfg)
	assert.NoError(t, err)
	defer gd.Close()

	stream, err := gd.Clients[0].Session(context.Background(), &api.SessionRequest{})
	assert.NoError(t, err)
	stream.CloseSend()
	resp, err := stream.Recv()
	assert.NoError(t, err)
	assert.NotEmpty(t, resp.SessionID)

	msg, err := stream.Recv()
	require.NoError(t, err)
	assert.Equal(t, 1, len(msg.Managers))
}

func TestSessionNoCert(t *testing.T) {
	cfg := DefaultConfig()
	gd, err := startDispatcher(cfg)
	assert.NoError(t, err)
	defer gd.Close()

	stream, err := gd.Clients[2].Session(context.Background(), &api.SessionRequest{})
	msg, err := stream.Recv()
	assert.Nil(t, msg)
	assert.EqualError(t, err, "rpc error: code = 7 desc = Permission denied: unauthorized peer role: rpc error: code = 7 desc = no client certificates in request")
}

func TestNodesCount(t *testing.T) {
	cfg := DefaultConfig()
	cfg.HeartbeatPeriod = 100 * time.Millisecond
	cfg.HeartbeatEpsilon = 0
	gd, err := startDispatcher(cfg)
	assert.NoError(t, err)
	defer gd.Close()

	{
		stream, err := gd.Clients[0].Session(context.Background(), &api.SessionRequest{})
		assert.NoError(t, err)
		defer stream.CloseSend()
		stream.Recv()
	}
	{
		stream, err := gd.Clients[1].Session(context.Background(), &api.SessionRequest{})
		assert.NoError(t, err)
		defer stream.CloseSend()
		stream.Recv()
	}
	assert.Equal(t, 6, gd.dispatcherServer.NodeCount())
	time.Sleep(500 * time.Millisecond)
	assert.Equal(t, 0, gd.dispatcherServer.NodeCount())
}
