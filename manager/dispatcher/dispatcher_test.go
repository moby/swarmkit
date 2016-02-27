package dispatcher

import (
	"net"
	"testing"
	"time"

	"golang.org/x/net/context"

	"google.golang.org/grpc"

	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/state"
	"github.com/stretchr/testify/assert"
)

type grpcDispatcher struct {
	Client      api.AgentClient
	Store       state.Store
	grpcServer  *grpc.Server
	agentServer api.AgentServer
	conn        *grpc.ClientConn
}

func (gd *grpcDispatcher) Close() {
	gd.conn.Close()
	gd.grpcServer.Stop()
}

func startDispatcher() (*grpcDispatcher, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	s := grpc.NewServer()
	store := state.NewMemoryStore()
	d := New(store)
	api.RegisterAgentServer(s, d)
	go s.Serve(l)
	conn, err := grpc.Dial(l.Addr().String(), grpc.WithInsecure(), grpc.WithTimeout(10*time.Second))
	if err != nil {
		s.Stop()
		return nil, err
	}
	cli := api.NewAgentClient(conn)
	return &grpcDispatcher{
		Client:      cli,
		Store:       store,
		agentServer: d,
		conn:        conn,
		grpcServer:  s,
	}, nil
}

func TestRegisterTwice(t *testing.T) {
	gd, err := startDispatcher()
	assert.Nil(t, err)
	defer gd.Close()

	testNode := &api.NodeSpec{ID: "test"}
	{
		resp, err := gd.Client.Register(context.Background(), &api.RegisterRequest{Spec: testNode})
		assert.Nil(t, err)
		assert.NotZero(t, resp.TTL)
	}
	{
		resp, err := gd.Client.Register(context.Background(), &api.RegisterRequest{Spec: testNode})
		assert.Nil(t, resp)
		assert.NotNil(t, err)
		assert.Equal(t, grpc.ErrorDesc(err), ErrNodeAlreadyRegistered.Error())
	}
}

func TestHeartbeat(t *testing.T) {
	origTTL := defaultTTL
	defaultTTL = 500 * time.Millisecond
	defer func() {
		defaultTTL = origTTL
	}()
	gd, err := startDispatcher()
	assert.Nil(t, err)
	defer gd.Close()

	testNode := &api.NodeSpec{ID: "test"}
	{
		resp, err := gd.Client.Register(context.Background(), &api.RegisterRequest{Spec: testNode})
		assert.Nil(t, err)
		assert.NotZero(t, resp.TTL)
	}
	time.Sleep(250 * time.Millisecond)

	resp, err := gd.Client.Heartbeat(context.Background(), &api.HeartbeatRequest{NodeID: testNode.ID})
	assert.Nil(t, err)
	assert.NotZero(t, resp.TTL)
	time.Sleep(300 * time.Millisecond)

	storeNode := gd.Store.Node("test")
	assert.NotNil(t, storeNode)
	assert.Equal(t, storeNode.Status.State, api.NodeStatus_READY)
}

func TestHeartbeatTimeout(t *testing.T) {
	origTTL := defaultTTL
	defaultTTL = 100 * time.Millisecond
	defer func() {
		defaultTTL = origTTL
	}()
	gd, err := startDispatcher()
	assert.Nil(t, err)
	defer gd.Close()

	testNode := &api.NodeSpec{ID: "test"}
	{
		resp, err := gd.Client.Register(context.Background(), &api.RegisterRequest{Spec: testNode})
		assert.Nil(t, err)
		assert.NotZero(t, resp.TTL)
	}
	time.Sleep(500 * time.Millisecond)

	storeNode := gd.Store.Node("test")
	assert.NotNil(t, storeNode)
	assert.Equal(t, api.NodeStatus_DOWN, storeNode.Status.State)

	// check that node is deregistered
	resp, err := gd.Client.Heartbeat(context.Background(), &api.HeartbeatRequest{NodeID: testNode.ID})
	assert.Nil(t, resp)
	assert.NotNil(t, err)
	assert.Equal(t, grpc.ErrorDesc(err), ErrNodeNotRegistered.Error())
}

func TestHeartbeatUnregistered(t *testing.T) {
	gd, err := startDispatcher()
	assert.Nil(t, err)
	defer gd.Close()
	resp, err := gd.Client.Heartbeat(context.Background(), &api.HeartbeatRequest{NodeID: "test"})
	assert.Nil(t, resp)
	assert.NotNil(t, err)
	assert.Equal(t, grpc.ErrorDesc(err), ErrNodeNotRegistered.Error())
}

func TestTasks(t *testing.T) {
	gd, err := startDispatcher()
	assert.Nil(t, err)
	defer gd.Close()
	testNode := &api.NodeSpec{ID: "test"}
	{
		resp, err := gd.Client.Register(context.Background(), &api.RegisterRequest{Spec: testNode})
		assert.Nil(t, err)
		assert.NotZero(t, resp.TTL)
	}
	testTask := &api.Task{
		ID:     "testTask",
		NodeID: testNode.ID,
	}
	stream, err := gd.Client.Tasks(context.Background(), &api.TasksRequest{NodeID: testNode.ID})
	assert.Nil(t, err)
	assert.Nil(t, gd.Store.CreateTask(testTask.ID, testTask))
	resp, err := stream.Recv()
	assert.Nil(t, err)
	assert.Equal(t, len(resp.Tasks), 1)
}

func TestTaskUpdate(t *testing.T) {
	gd, err := startDispatcher()
	assert.Nil(t, err)
	defer gd.Close()

	testNode := &api.NodeSpec{ID: "test"}
	{
		resp, err := gd.Client.Register(context.Background(), &api.RegisterRequest{Spec: testNode})
		assert.Nil(t, err)
		assert.NotZero(t, resp.TTL)
	}
	testTask1 := &api.Task{
		ID:     "testTask1",
		NodeID: testNode.ID,
	}
	testTask2 := &api.Task{
		ID:     "testTask2",
		NodeID: testNode.ID,
	}
	assert.Nil(t, gd.Store.CreateTask(testTask1.ID, testTask1))
	assert.Nil(t, gd.Store.CreateTask(testTask2.ID, testTask2))
	testTask1.Status = &api.TaskStatus{State: api.TaskStatus_ASSIGNED}
	testTask2.Status = &api.TaskStatus{State: api.TaskStatus_ASSIGNED}
	updReq := &api.UpdateTaskStatusRequest{
		NodeID: testNode.ID,
		Tasks:  []*api.Task{testTask1, testTask2},
	}
	_, err = gd.Client.UpdateTaskStatus(context.Background(), updReq)
	assert.Nil(t, err)
	storeTask1 := gd.Store.Task(testTask1.ID)
	assert.NotNil(t, storeTask1)
	storeTask2 := gd.Store.Task(testTask2.ID)
	assert.NotNil(t, storeTask2)
	assert.Equal(t, storeTask1.Status.State, api.TaskStatus_ASSIGNED)
	assert.Equal(t, storeTask2.Status.State, api.TaskStatus_ASSIGNED)
}
