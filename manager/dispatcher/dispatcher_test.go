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

	"github.com/docker/go-events"
	"github.com/docker/swarmkit/api"
	"github.com/docker/swarmkit/ca"
	"github.com/docker/swarmkit/ca/testutils"
	raftutils "github.com/docker/swarmkit/manager/state/raft/testutils"
	"github.com/docker/swarmkit/manager/state/store"
	"github.com/stretchr/testify/assert"
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
			NodeID: "1",
			Addr:   t.addr,
		},
	}
}

func (t *testCluster) SubscribePeers() (chan events.Event, func()) {
	ch := make(chan events.Event, 1)
	ch <- []*api.Peer{
		{
			Addr:   t.addr,
			NodeID: "1",
		},
	}
	return ch, func() {
		close(ch)
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
	agentSecurityConfig1, err := tca.NewNodeConfig(ca.WorkerRole)
	if err != nil {
		return nil, err
	}
	agentSecurityConfig2, err := tca.NewNodeConfig(ca.WorkerRole)
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
		_, err := ca.AuthorizeForwardedRoleAndOrg(ctx, roles, []string{ca.ManagerRole}, tca.Organization, nil)
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
	t.Parallel()

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
		assert.Equal(t, codes.Unavailable, grpc.Code(err), err.Error())
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
	t.Parallel()

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
		storeNodes, err := store.FindNodes(readTx, store.ByIDPrefix(gd.SecurityConfigs[0].ClientTLSCreds.NodeID()))
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

// If the session ID is not sent as part of the Assignments request, an error is returned to the stream
func TestAssignmentsErrorsIfNoSessionID(t *testing.T) {
	t.Parallel()

	gd, err := startDispatcher(DefaultConfig())
	assert.NoError(t, err)
	defer gd.Close()

	// without correct SessionID should fail
	stream, err := gd.Clients[0].Assignments(context.Background(), &api.AssignmentsRequest{})
	assert.NoError(t, err)
	assert.NotNil(t, stream)
	defer stream.CloseSend()

	resp, err := stream.Recv()
	assert.Nil(t, resp)
	assert.Error(t, err)
	assert.Equal(t, grpc.Code(err), codes.InvalidArgument)
}

// Assignments will send down any existing node tasks > ASSIGNED, and any secrets
// for said tasks that are <= RUNNING (if the secrets exist)
func TestAssignmentsInitialNodeTasks(t *testing.T) {
	t.Parallel()

	gd, err := startDispatcher(DefaultConfig())
	assert.NoError(t, err)
	defer gd.Close()

	expectedSessionID, nodeID := getSessionAndNodeID(t, gd.Clients[0])

	// create the relevant secrets and tasks
	secrets, tasks := makeTasksAndSecrets(t, nodeID)
	err = gd.Store.Update(func(tx store.Tx) error {
		for _, secret := range secrets[:] {
			assert.NoError(t, store.CreateSecret(tx, secret))
		}
		for _, task := range tasks {
			assert.NoError(t, store.CreateTask(tx, task))
		}
		return nil
	})
	assert.NoError(t, err)

	stream, err := gd.Clients[0].Assignments(context.Background(), &api.AssignmentsRequest{SessionID: expectedSessionID})
	assert.NoError(t, err)
	defer stream.CloseSend()

	time.Sleep(100 * time.Millisecond)

	// check the initial task and secret stream
	resp, err := stream.Recv()
	assert.NoError(t, err)

	// FIXME(aaronl): This is hard to maintain.
	assert.Equal(t, 17, len(resp.Changes))

	taskChanges, secretChanges := collectTasksAndSecrets(resp.Changes)
	assert.Len(t, taskChanges, 10) // 10 types of task states >= assigned, 2 types < assigned
	for _, task := range tasks[2:] {
		assert.NotNil(t, taskChanges[idAndAction{id: task.ID, action: api.AssignmentChange_AssignmentActionUpdate}])
	}
	assert.Len(t, secretChanges, 7) // 6 different secrets for states between assigned and running inclusive plus secret12
	for _, secret := range secrets[2:8] {
		assert.NotNil(t, secretChanges[idAndAction{id: secret.ID, action: api.AssignmentChange_AssignmentActionUpdate}])
	}

	// updating all the tasks will attempt to remove all the secrets for the tasks that are in state > running
	err = gd.Store.Update(func(tx store.Tx) error {
		for _, task := range tasks {
			assert.NoError(t, store.UpdateTask(tx, task))
		}
		return nil

	})
	assert.NoError(t, err)

	// updates for all the tasks, remove secret sent for the 4 types of states > running
	resp, err = stream.Recv()
	assert.NoError(t, err)

	assert.Equal(t, 5, len(resp.Changes))
	taskChanges, secretChanges = collectTasksAndSecrets(resp.Changes)
	assert.Len(t, taskChanges, 1)
	assert.NotNil(t, taskChanges[idAndAction{id: tasks[2].ID, action: api.AssignmentChange_AssignmentActionUpdate}]) // this is the task in ASSIGNED

	assert.Len(t, secretChanges, 4) // these are the secrets for states > running
	for _, secret := range secrets[9 : len(secrets)-1] {
		assert.NotNil(t, secretChanges[idAndAction{id: secret.ID, action: api.AssignmentChange_AssignmentActionRemove}])
	}

	// deleting the tasks removes all the secrets for every single task, no matter
	// what state it's in
	err = gd.Store.Update(func(tx store.Tx) error {
		for _, task := range tasks {
			assert.NoError(t, store.DeleteTask(tx, task.ID))
		}
		return nil
	})
	assert.NoError(t, err)

	// updates for all the tasks >= ASSIGNMENT, and remove secrets for all of them,
	// (there will be 2 tasks changes that won't be sent down)
	resp, err = stream.Recv()
	assert.NoError(t, err)
	assert.Equal(t, len(tasks)-2+len(secrets)-2, len(resp.Changes))
	taskChanges, secretChanges = collectTasksAndSecrets(resp.Changes)
	assert.Len(t, taskChanges, len(tasks)-2)
	for _, task := range tasks[2:] {
		assert.NotNil(t, taskChanges[idAndAction{id: task.ID, action: api.AssignmentChange_AssignmentActionRemove}])
	}

	assert.Len(t, secretChanges, len(secrets)-2)
	for _, secret := range secrets[2:] {
		assert.NotNil(t, secretChanges[idAndAction{id: secret.ID, action: api.AssignmentChange_AssignmentActionRemove}])
	}
}

// As tasks are added, assignments will send down tasks > ASSIGNED, and any secrets
// for said tasks that are <= RUNNING (if the secrets exist)
func TestAssignmentsAddingTasks(t *testing.T) {
	t.Parallel()

	gd, err := startDispatcher(DefaultConfig())
	assert.NoError(t, err)
	defer gd.Close()

	expectedSessionID, nodeID := getSessionAndNodeID(t, gd.Clients[0])

	stream, err := gd.Clients[0].Assignments(context.Background(), &api.AssignmentsRequest{SessionID: expectedSessionID})
	assert.NoError(t, err)
	defer stream.CloseSend()

	time.Sleep(100 * time.Millisecond)

	// There are no initial tasks or secrets
	resp, err := stream.Recv()
	assert.NoError(t, err)
	assert.Empty(t, resp.Changes)

	// create the relevant secrets and tasks and update the tasks
	secrets, tasks := makeTasksAndSecrets(t, nodeID)
	err = gd.Store.Update(func(tx store.Tx) error {
		for _, secret := range secrets[:len(secrets)-1] {
			assert.NoError(t, store.CreateSecret(tx, secret))
		}
		for _, task := range tasks {
			assert.NoError(t, store.CreateTask(tx, task))
		}
		return nil
	})
	assert.NoError(t, err)

	// Nothing happens until we update.  Updating all the tasks will send updates for all the tasks >= ASSIGNED (10),
	// and secrets for all the tasks >= ASSIGNED and <= RUNNING (6).
	err = gd.Store.Update(func(tx store.Tx) error {
		for _, task := range tasks {
			assert.NoError(t, store.UpdateTask(tx, task))
		}
		return nil

	})
	assert.NoError(t, err)

	resp, err = stream.Recv()
	assert.NoError(t, err)

	// FIXME(aaronl): This is hard to maintain.
	assert.Equal(t, 10+6, len(resp.Changes))
	taskChanges, secretChanges := collectTasksAndSecrets(resp.Changes)
	assert.Len(t, taskChanges, 10)
	for _, task := range tasks[2:] {
		assert.NotNil(t, taskChanges[idAndAction{id: task.ID, action: api.AssignmentChange_AssignmentActionUpdate}])
	}

	assert.Len(t, secretChanges, 6)
	// all the secrets for tasks >= ASSIGNED and <= RUNNING
	for _, secret := range secrets[2:8] {
		assert.NotNil(t, secretChanges[idAndAction{id: secret.ID, action: api.AssignmentChange_AssignmentActionUpdate}])
	}

	// deleting the tasks removes all the secrets for every single task, no matter
	// what state it's in
	err = gd.Store.Update(func(tx store.Tx) error {
		for _, task := range tasks {
			assert.NoError(t, store.DeleteTask(tx, task.ID))
		}
		return nil

	})
	assert.NoError(t, err)

	// updates for all the tasks >= ASSIGNMENT, and remove secrets for all of them, even ones that don't exist
	// (there will be 2 tasks changes that won't be sent down)
	resp, err = stream.Recv()
	assert.NoError(t, err)

	assert.Equal(t, len(tasks)-2+len(secrets)-2, len(resp.Changes))
	taskChanges, secretChanges = collectTasksAndSecrets(resp.Changes)
	assert.Len(t, taskChanges, len(tasks)-2)
	for _, task := range tasks[2:] {
		assert.NotNil(t, taskChanges[idAndAction{id: task.ID, action: api.AssignmentChange_AssignmentActionRemove}])
	}

	assert.Len(t, secretChanges, len(secrets)-2)
	for _, secret := range secrets[2:] {
		assert.NotNil(t, secretChanges[idAndAction{id: secret.ID, action: api.AssignmentChange_AssignmentActionRemove}])
	}
}

// If a secret is updated or deleted, even if it's for an existing task, no changes will be sent down
func TestAssignmentsSecretUpdateAndDeletion(t *testing.T) {
	t.Parallel()

	gd, err := startDispatcher(DefaultConfig())
	assert.NoError(t, err)
	defer gd.Close()

	expectedSessionID, nodeID := getSessionAndNodeID(t, gd.Clients[0])

	// create the relevant secrets and tasks
	secrets, tasks := makeTasksAndSecrets(t, nodeID)
	err = gd.Store.Update(func(tx store.Tx) error {
		for _, secret := range secrets[:len(secrets)-1] {
			assert.NoError(t, store.CreateSecret(tx, secret))
		}
		for _, task := range tasks {
			assert.NoError(t, store.CreateTask(tx, task))
		}
		return nil
	})
	assert.NoError(t, err)

	stream, err := gd.Clients[0].Assignments(context.Background(), &api.AssignmentsRequest{SessionID: expectedSessionID})
	assert.NoError(t, err)
	defer stream.CloseSend()

	time.Sleep(100 * time.Millisecond)

	// check the initial task and secret stream
	resp, err := stream.Recv()
	assert.NoError(t, err)

	// FIXME(aaronl): This is hard to maintain.
	assert.Equal(t, 16, len(resp.Changes))
	taskChanges, secretChanges := collectTasksAndSecrets(resp.Changes)
	assert.Len(t, taskChanges, 10) // 10 types of task states >= assigned, 2 types < assigned
	for _, task := range tasks[2:] {
		assert.NotNil(t, taskChanges[idAndAction{id: task.ID, action: api.AssignmentChange_AssignmentActionUpdate}])
	}
	assert.Len(t, secretChanges, 6) // 6 types of task states between assigned and running inclusive
	for _, secret := range secrets[2:8] {
		assert.NotNil(t, secretChanges[idAndAction{id: secret.ID, action: api.AssignmentChange_AssignmentActionUpdate}])
	}

	// updating secrets, used by tasks or not, do not cause any changes
	err = gd.Store.Update(func(tx store.Tx) error {
		for _, secret := range secrets[:len(secrets)-2] {
			secret.Spec.Data = []byte("new secret data")
			assert.NoError(t, store.UpdateSecret(tx, secret))
		}
		return nil
	})
	assert.NoError(t, err)

	recvChan := make(chan struct{})
	go func() {
		_, _ = stream.Recv()
		recvChan <- struct{}{}
	}()

	select {
	case <-recvChan:
		assert.Fail(t, "secret update should not trigger dispatcher update")
	case <-time.After(250 * time.Millisecond):
	}

	// deleting secrets, used by tasks or not, do not cause any changes
	err = gd.Store.Update(func(tx store.Tx) error {
		for _, secret := range secrets[:len(secrets)-2] {
			assert.NoError(t, store.DeleteSecret(tx, secret.ID))
		}
		return nil
	})
	assert.NoError(t, err)

	select {
	case <-recvChan:
		assert.Fail(t, "secret delete should not trigger dispatcher update")
	case <-time.After(250 * time.Millisecond):
	}
}

func TestTasksStatusChange(t *testing.T) {
	t.Parallel()

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

	stream, err := gd.Clients[0].Assignments(context.Background(), &api.AssignmentsRequest{SessionID: expectedSessionID})
	assert.NoError(t, err)

	time.Sleep(100 * time.Millisecond)

	resp, err := stream.Recv()
	assert.NoError(t, err)
	// initially no tasks
	assert.Equal(t, 0, len(resp.Changes))

	// Creating the tasks will not create an event for assignments
	err = gd.Store.Update(func(tx store.Tx) error {
		assert.NoError(t, store.CreateTask(tx, testTask1))
		assert.NoError(t, store.CreateTask(tx, testTask2))
		return nil
	})
	assert.NoError(t, err)
	err = gd.Store.Update(func(tx store.Tx) error {
		assert.NoError(t, store.UpdateTask(tx, testTask1))
		assert.NoError(t, store.UpdateTask(tx, testTask2))
		return nil
	})
	assert.NoError(t, err)

	resp, err = stream.Recv()
	assert.NoError(t, err)
	assert.Equal(t, len(resp.Changes), 2)
	tasks, secrets := collectTasksAndSecrets(resp.Changes)
	assert.Len(t, tasks, 2)
	assert.Len(t, secrets, 0)
	assert.NotNil(t, tasks[idAndAction{id: "testTask1", action: api.AssignmentChange_AssignmentActionUpdate}])
	assert.NotNil(t, tasks[idAndAction{id: "testTask2", action: api.AssignmentChange_AssignmentActionUpdate}])

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

	stream, err := gd.Clients[0].Assignments(context.Background(), &api.AssignmentsRequest{SessionID: expectedSessionID})
	assert.NoError(t, err)

	resp, err := stream.Recv()
	assert.NoError(t, err)
	// initially no tasks
	assert.Equal(t, 0, len(resp.Changes))

	// Create, Update and Delete tasks.
	err = gd.Store.Update(func(tx store.Tx) error {
		assert.NoError(t, store.CreateTask(tx, testTask1))
		assert.NoError(t, store.CreateTask(tx, testTask2))
		return nil
	})
	assert.NoError(t, err)
	err = gd.Store.Update(func(tx store.Tx) error {
		assert.NoError(t, store.UpdateTask(tx, testTask1))
		assert.NoError(t, store.UpdateTask(tx, testTask2))
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

	tasks, secrets := collectTasksAndSecrets(resp.Changes)
	assert.Len(t, tasks, 2)
	assert.Len(t, secrets, 0)
	assert.Equal(t, api.AssignmentChange_AssignmentActionRemove, resp.Changes[0].Action)
	assert.Equal(t, api.AssignmentChange_AssignmentActionRemove, resp.Changes[1].Action)
}

func TestTasksNoCert(t *testing.T) {
	gd, err := startDispatcher(DefaultConfig())
	assert.NoError(t, err)
	defer gd.Close()

	stream, err := gd.Clients[2].Assignments(context.Background(), &api.AssignmentsRequest{})
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
	// testTask1 and testTask2 are advanced from NEW to ASSIGNED.
	testTask1 := &api.Task{
		ID:     "testTask1",
		NodeID: nodeID,
	}
	testTask2 := &api.Task{
		ID:     "testTask2",
		NodeID: nodeID,
	}
	// testTask3 is used to confirm that status updates for a task not
	// assigned to the node sending the update are rejected.
	testTask3 := &api.Task{
		ID:     "testTask3",
		NodeID: "differentnode",
	}
	// testTask4 is used to confirm that a task's state is not allowed to
	// move backwards.
	testTask4 := &api.Task{
		ID:     "testTask4",
		NodeID: nodeID,
		Status: api.TaskStatus{
			State: api.TaskStateShutdown,
		},
	}
	err = gd.Store.Update(func(tx store.Tx) error {
		assert.NoError(t, store.CreateTask(tx, testTask1))
		assert.NoError(t, store.CreateTask(tx, testTask2))
		assert.NoError(t, store.CreateTask(tx, testTask3))
		assert.NoError(t, store.CreateTask(tx, testTask4))
		return nil
	})
	assert.NoError(t, err)

	testTask1.Status = api.TaskStatus{State: api.TaskStateAssigned}
	testTask2.Status = api.TaskStatus{State: api.TaskStateAssigned}
	testTask3.Status = api.TaskStatus{State: api.TaskStateAssigned}
	testTask4.Status = api.TaskStatus{State: api.TaskStateRunning}
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
			{
				TaskID: testTask4.ID,
				Status: &testTask4.Status,
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

	gd.dispatcherServer.processUpdates(context.Background())

	gd.Store.View(func(readTx store.ReadTx) {
		storeTask1 := store.GetTask(readTx, testTask1.ID)
		assert.NotNil(t, storeTask1)
		storeTask2 := store.GetTask(readTx, testTask2.ID)
		assert.NotNil(t, storeTask2)
		assert.Equal(t, storeTask1.Status.State, api.TaskStateAssigned)
		assert.Equal(t, storeTask2.Status.State, api.TaskStateAssigned)

		storeTask3 := store.GetTask(readTx, testTask3.ID)
		assert.NotNil(t, storeTask3)
		assert.Equal(t, storeTask3.Status.State, api.TaskStateNew)

		// The update to task4's state should be ignored because it
		// would have moved backwards.
		storeTask4 := store.GetTask(readTx, testTask4.ID)
		assert.NotNil(t, storeTask4)
		assert.Equal(t, storeTask4.Status.State, api.TaskStateShutdown)
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
	assert.Equal(t, 1, len(resp.Managers))
}

func TestSessionNoCert(t *testing.T) {
	cfg := DefaultConfig()
	gd, err := startDispatcher(cfg)
	assert.NoError(t, err)
	defer gd.Close()

	stream, err := gd.Clients[2].Session(context.Background(), &api.SessionRequest{})
	assert.NoError(t, err)
	msg, err := stream.Recv()
	assert.Nil(t, msg)
	assert.EqualError(t, err, "rpc error: code = 7 desc = Permission denied: unauthorized peer role: rpc error: code = 7 desc = no client certificates in request")
}

func getSessionAndNodeID(t *testing.T, c api.DispatcherClient) (string, string) {
	stream, err := c.Session(context.Background(), &api.SessionRequest{})
	assert.NoError(t, err)
	defer stream.CloseSend()
	resp, err := stream.Recv()
	assert.NoError(t, err)
	assert.NotEmpty(t, resp.SessionID)
	return resp.SessionID, resp.Node.ID
}

type idAndAction struct {
	id     string
	action api.AssignmentChange_AssignmentAction
}

func collectTasksAndSecrets(changes []*api.AssignmentChange) (map[idAndAction]*api.Task, map[idAndAction]*api.Secret) {
	tasks := make(map[idAndAction]*api.Task)
	secrets := make(map[idAndAction]*api.Secret)
	for _, change := range changes {
		task := change.Assignment.GetTask()
		if task != nil {
			tasks[idAndAction{id: task.ID, action: change.Action}] = task
		}
		secret := change.Assignment.GetSecret()
		if secret != nil {
			secrets[idAndAction{id: secret.ID, action: change.Action}] = secret
		}
	}

	return tasks, secrets
}

func makeTasksAndSecrets(t *testing.T, nodeID string) ([]*api.Secret, []*api.Task) {
	var secrets []*api.Secret
	var tasks []*api.Task
	for i := 0; i <= len(taskStatesInOrder); i++ {
		secrets = append(secrets, &api.Secret{
			ID: fmt.Sprintf("IDsecret%d", i),
			Spec: api.SecretSpec{
				Annotations: api.Annotations{
					Name: fmt.Sprintf("secret%d", i),
				},
				Data: []byte(fmt.Sprintf("secret%d", i)),
			},
		})
	}
	for i, taskState := range taskStatesInOrder {
		tasks = append(tasks, &api.Task{
			NodeID:       nodeID,
			ID:           fmt.Sprintf("testTask%d", i),
			Status:       api.TaskStatus{State: taskState},
			DesiredState: api.TaskStateReady,
			Spec:         taskSpecFromSecrets(secrets[i], secrets[len(secrets)-1]),
		})
	}
	return secrets, tasks
}

func taskSpecFromSecrets(secrets ...*api.Secret) api.TaskSpec {
	var secretRefs []*api.SecretReference
	for _, s := range secrets {
		secretRefs = append(secretRefs, &api.SecretReference{
			SecretName: s.Spec.Annotations.Name,
			SecretID:   s.ID,
			Target: &api.SecretReference_File{
				File: &api.SecretReference_FileTarget{
					Name: "target.txt",
					UID:  "0",
					GID:  "0",
					Mode: 0666,
				},
			},
		})
	}
	return api.TaskSpec{
		Runtime: &api.TaskSpec_Container{
			Container: &api.ContainerSpec{
				Secrets: secretRefs,
			},
		},
	}
}

var taskStatesInOrder = []api.TaskState{
	api.TaskStateNew,
	api.TaskStatePending,
	api.TaskStateAssigned,
	api.TaskStateAccepted,
	api.TaskStatePreparing,
	api.TaskStateReady,
	api.TaskStateStarting,
	api.TaskStateRunning,
	api.TaskStateCompleted,
	api.TaskStateShutdown,
	api.TaskStateFailed,
	api.TaskStateRejected,
}

// Ensure we test the old Tasks() API for backwards compat

func TestOldTasks(t *testing.T) {
	t.Parallel()

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

func TestOldTasksStatusChange(t *testing.T) {
	t.Parallel()

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

func TestOldTasksBatch(t *testing.T) {
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

func TestOldTasksNoCert(t *testing.T) {
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
