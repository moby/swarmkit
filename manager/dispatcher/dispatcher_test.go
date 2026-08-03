package dispatcher

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"

	"github.com/docker/go-events"
	"github.com/moby/swarmkit/v2/api"
	"github.com/moby/swarmkit/v2/ca"
	cautils "github.com/moby/swarmkit/v2/ca/testutils"
	"github.com/moby/swarmkit/v2/identity"
	"github.com/moby/swarmkit/v2/manager/drivers"
	"github.com/moby/swarmkit/v2/manager/state/store"
	"github.com/moby/swarmkit/v2/node/plugin"
	"github.com/moby/swarmkit/v2/testutils"
	digest "github.com/opencontainers/go-digest"
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
	testCA           *cautils.TestCA
	testCluster      *testCluster
	PluginGetter     *mockPluginGetter
}

func (gd *grpcDispatcher) Close() {
	// Close the client connection.
	for _, conn := range gd.conns {
		conn.Close()
	}
	gd.dispatcherServer.Stop()
	gd.grpcServer.Stop()
	gd.testCA.Stop()
}

type testCluster struct {
	mu            sync.Mutex
	addr          string
	store         *store.MemoryStore
	subscriptions map[string]chan events.Event
	peers         []*api.Peer
	members       map[uint64]*api.RaftMember
}

func newTestCluster(addr string, s *store.MemoryStore) *testCluster {
	return &testCluster{
		addr:          addr,
		store:         s,
		subscriptions: make(map[string]chan events.Event),
		peers: []*api.Peer{
			{
				Addr:   addr,
				NodeId: "1",
			},
		},
		members: map[uint64]*api.RaftMember{
			1: {
				NodeId: "1",
				Addr:   addr,
			},
		},
	}
}

func (t *testCluster) GetMemberlist() map[uint64]*api.RaftMember {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.members
}

func (t *testCluster) SubscribePeers() (chan events.Event, func()) {
	t.mu.Lock()
	defer t.mu.Unlock()
	ch := make(chan events.Event, 1)
	id := identity.NewID()
	t.subscriptions[id] = ch
	ch <- t.peers
	return ch, func() {
		t.mu.Lock()
		defer t.mu.Unlock()
		delete(t.subscriptions, id)
		close(ch)
	}
}

func (t *testCluster) addMember(addr string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	id := uint64(len(t.members) + 1)
	strID := fmt.Sprintf("%d", id)
	t.members[id] = &api.RaftMember{
		NodeId: strID,
		Addr:   addr,
	}
	t.peers = append(t.peers, &api.Peer{
		Addr:   addr,
		NodeId: strID,
	})
	for _, ch := range t.subscriptions {
		ch <- t.peers
	}
}

func (t *testCluster) MemoryStore() *store.MemoryStore {
	return t.store
}

func startDispatcher(t *testing.T, c *Config) *grpcDispatcher {
	t.Helper()

	l, err := net.Listen("tcp", "127.0.0.1:0")
	assert.NoError(t, err)

	tca := cautils.NewTestCA(t)
	tca.CAServer.Stop() // there is no need for the CA server to be running
	agentSecurityConfig1, err := tca.NewNodeConfig(ca.WorkerRole)
	assert.NoError(t, err)

	agentSecurityConfig2, err := tca.NewNodeConfig(ca.WorkerRole)
	assert.NoError(t, err)

	managerSecurityConfig, err := tca.NewNodeConfig(ca.ManagerRole)
	assert.NoError(t, err)

	serverOpts := []grpc.ServerOption{grpc.Creds(managerSecurityConfig.ServerTLSCreds)}

	s := grpc.NewServer(serverOpts...)
	tc := newTestCluster(l.Addr().String(), tca.MemoryStore)
	driverGetter := &mockPluginGetter{}
	d := New()
	d.Init(tc, c, drivers.New(driverGetter), managerSecurityConfig)
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
	err = testutils.PollFuncWithTimeout(nil, func() error {
		d.mu.Lock()
		defer d.mu.Unlock()
		if !d.isRunning() {
			return fmt.Errorf("dispatcher is not running")
		}
		return nil
	}, 5*time.Second)
	assert.NoError(t, err)

	clientOpts := []grpc.DialOption{grpc.WithTimeout(10 * time.Second)}
	clientOpts1 := append(clientOpts, grpc.WithTransportCredentials(agentSecurityConfig1.ClientTLSCreds))
	clientOpts2 := append(clientOpts, grpc.WithTransportCredentials(agentSecurityConfig2.ClientTLSCreds))
	clientOpts3 := append(clientOpts, grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{InsecureSkipVerify: true})))

	conn1, err := grpc.Dial(l.Addr().String(), clientOpts1...)
	assert.NoError(t, err)

	conn2, err := grpc.Dial(l.Addr().String(), clientOpts2...)
	assert.NoError(t, err)

	conn3, err := grpc.Dial(l.Addr().String(), clientOpts3...)
	assert.NoError(t, err)

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
		testCluster:      tc,
		PluginGetter:     driverGetter,
	}
}

func TestRegisterTwice(t *testing.T) {
	cfg := DefaultConfig()
	cfg.RateLimitPeriod = 0
	gd := startDispatcher(t, cfg)
	defer gd.Close()

	var expectedSessionID string
	{
		stream, err := gd.Clients[0].Session(context.Background(), &api.SessionRequest{})
		assert.NoError(t, err)
		msg, err := stream.Recv()
		assert.NoError(t, err)
		assert.NotEmpty(t, msg.SessionId)
		expectedSessionID = msg.SessionId
		stream.CloseSend()
	}
	{
		stream, err := gd.Clients[0].Session(context.Background(), &api.SessionRequest{})
		assert.NoError(t, err)
		msg, err := stream.Recv()

		assert.NoError(t, err)
		// session should be different!
		assert.NotEqual(t, msg.SessionId, expectedSessionID)
		stream.CloseSend()
	}
}

func TestRegisterExceedRateLimit(t *testing.T) {
	t.Parallel()

	gd := startDispatcher(t, DefaultConfig())
	defer gd.Close()

	for range 3 {
		stream, err := gd.Clients[0].Session(context.Background(), &api.SessionRequest{})
		assert.NoError(t, err)
		msg, err := stream.Recv()
		assert.NoError(t, err)
		assert.NotEmpty(t, msg.SessionId)
		stream.CloseSend()
	}
	{
		stream, err := gd.Clients[0].Session(context.Background(), &api.SessionRequest{})
		defer stream.CloseSend()
		assert.NoError(t, err)
		_, err = stream.Recv()
		assert.Error(t, err)
		assert.Equal(t, codes.Unavailable, testutils.ErrorCode(err), err.Error())
	}
}

func TestRegisterNoCert(t *testing.T) {
	gd := startDispatcher(t, DefaultConfig())
	defer gd.Close()

	// This client has no certificates, this should fail
	stream, err := gd.Clients[2].Session(context.Background(), &api.SessionRequest{})
	assert.NoError(t, err)
	defer stream.CloseSend()
	resp, err := stream.Recv()
	assert.Nil(t, resp)
	assert.EqualError(t, err, "rpc error: code = PermissionDenied desc = Permission denied: unauthorized peer role: rpc error: code = PermissionDenied desc = no client certificates in request")
}

func TestHeartbeat(t *testing.T) {
	cfg := DefaultConfig()
	cfg.HeartbeatPeriod = 500 * time.Millisecond
	cfg.HeartbeatEpsilon = 0
	gd := startDispatcher(t, DefaultConfig())
	defer gd.Close()

	var expectedSessionID string
	{
		stream, err := gd.Clients[0].Session(context.Background(), &api.SessionRequest{})
		assert.NoError(t, err)
		defer stream.CloseSend()

		resp, err := stream.Recv()
		assert.NoError(t, err)
		assert.NotEmpty(t, resp.SessionId)
		expectedSessionID = resp.SessionId
	}
	time.Sleep(250 * time.Millisecond)

	{
		// heartbeat without correct SessionID should fail
		resp, err := gd.Clients[0].Heartbeat(context.Background(), &api.HeartbeatRequest{})
		assert.Nil(t, resp)
		assert.Error(t, err)
		assert.Equal(t, testutils.ErrorCode(err), codes.InvalidArgument)
	}

	resp, err := gd.Clients[0].Heartbeat(context.Background(), &api.HeartbeatRequest{SessionId: expectedSessionID})
	assert.NoError(t, err)
	assert.NotZero(t, resp.Period)
	time.Sleep(300 * time.Millisecond)

	gd.Store.View(func(readTx store.ReadTx) {
		storeNodes, err := store.FindNodes(readTx, store.All)
		assert.NoError(t, err)
		assert.NotEmpty(t, storeNodes)
		found := false
		for _, node := range storeNodes {
			if node.Id == gd.SecurityConfigs[0].ClientTLSCreds.NodeID() {
				found = true
				assert.Equal(t, api.NodeStatus_READY, node.Status.GetState())
			}
		}
		assert.True(t, found)
	})
}

func TestHeartbeatNoCert(t *testing.T) {
	gd := startDispatcher(t, DefaultConfig())
	defer gd.Close()

	// heartbeat without correct SessionID should fail
	resp, err := gd.Clients[2].Heartbeat(context.Background(), &api.HeartbeatRequest{})
	assert.Nil(t, resp)
	assert.EqualError(t, err, "rpc error: code = PermissionDenied desc = Permission denied: unauthorized peer role: rpc error: code = PermissionDenied desc = no client certificates in request")
}

func TestHeartbeatTimeout(t *testing.T) {
	t.Parallel()

	cfg := DefaultConfig()
	cfg.HeartbeatPeriod = 100 * time.Millisecond
	cfg.HeartbeatEpsilon = 0
	gd := startDispatcher(t, cfg)
	defer gd.Close()

	var expectedSessionID string
	{
		stream, err := gd.Clients[0].Session(context.Background(), &api.SessionRequest{})
		assert.NoError(t, err)
		resp, err := stream.Recv()
		assert.NoError(t, err)
		assert.NotEmpty(t, resp.SessionId)
		expectedSessionID = resp.SessionId

	}

	assert.NoError(t, testutils.PollFunc(nil, func() error {
		var storeNode *api.Node
		gd.Store.View(func(readTx store.ReadTx) {
			storeNode = store.GetNode(readTx, gd.SecurityConfigs[0].ClientTLSCreds.NodeID())
		})
		if storeNode == nil {
			return errors.New("node not found")
		}
		if storeNode.Status.GetState() != api.NodeStatus_DOWN {
			return errors.New("node is not down")
		}
		return nil
	}))

	// check that node is deregistered
	resp, err := gd.Clients[0].Heartbeat(context.Background(), &api.HeartbeatRequest{SessionId: expectedSessionID})
	assert.Nil(t, resp)
	assert.Error(t, err)
	assert.Equal(t, testutils.ErrorDesc(err), ErrNodeNotRegistered.Error())
}

func TestHeartbeatUnregistered(t *testing.T) {
	gd := startDispatcher(t, DefaultConfig())
	defer gd.Close()
	resp, err := gd.Clients[0].Heartbeat(context.Background(), &api.HeartbeatRequest{})
	assert.Nil(t, resp)
	assert.Error(t, err)
	assert.Equal(t, ErrSessionInvalid.Error(), testutils.ErrorDesc(err))
}

// If the session ID is not sent as part of the Assignments request, an error is returned to the stream
func TestAssignmentsErrorsIfNoSessionID(t *testing.T) {
	t.Parallel()

	gd := startDispatcher(t, DefaultConfig())
	defer gd.Close()

	// without correct SessionID should fail
	stream, err := gd.Clients[0].Assignments(context.Background(), &api.AssignmentsRequest{})
	assert.NoError(t, err)
	assert.NotNil(t, stream)
	defer stream.CloseSend()

	resp, err := stream.Recv()
	assert.Nil(t, resp)
	assert.Error(t, err)
	assert.Equal(t, testutils.ErrorCode(err), codes.InvalidArgument)
}

func TestAssignmentsSecretDriver(t *testing.T) {
	t.Parallel()

	const (
		secretDriver                 = "secret-driver"
		existingSecretName           = "existing-secret"
		doNotReuseExistingSecretName = "do-not-reuse-existing-secret"
		errSecretName                = "err-secret"
		serviceName                  = "service-name"
		serviceHostname              = "service-hostname"
		serviceEndpointMode          = 2
	)
	secretValue := []byte("custom-secret-value")
	doNotReuseSecretValue := []byte("custom-do-not-reuse-secret-value")
	serviceLabels := map[string]string{
		"label-name": "label-value",
	}

	portConfig := drivers.PortConfig{Name: "port", PublishMode: 5, TargetPort: 80, Protocol: 10, PublishedPort: 8080}

	responses := map[string]*drivers.SecretsProviderResponse{
		existingSecretName:           {Value: secretValue},
		doNotReuseExistingSecretName: {Value: doNotReuseSecretValue, DoNotReuse: true},
		errSecretName:                {Err: "Error from driver"},
	}

	var mux MockPluginClient
	mux.HandleFunc(drivers.SecretsProviderAPI, func(body []byte) (any, error) {
		var request drivers.SecretsProviderRequest
		assert.NoError(t, json.Unmarshal(body, &request))
		response := responses[request.SecretName]
		assert.Equal(t, serviceName, request.ServiceName)
		assert.Equal(t, serviceHostname, request.ServiceHostname)
		assert.Equal(t, int32(serviceEndpointMode), request.ServiceEndpointSpec.Mode)
		assert.Len(t, request.ServiceEndpointSpec.Ports, 1)
		assert.EqualValues(t, portConfig, request.ServiceEndpointSpec.Ports[0])
		assert.EqualValues(t, serviceLabels, request.ServiceLabels)
		assert.NotNil(t, response)
		return response, nil
	})

	gd := startDispatcher(t, DefaultConfig())
	defer gd.Close()
	assert.NoError(t, gd.PluginGetter.SetupPlugin(secretDriver, &mux))

	expectedSessionID, nodeID := getSessionAndNodeID(t, gd.Clients[0])

	secret := &api.Secret{
		Id: "driverSecret",
		Spec: &api.SecretSpec{
			Annotations: &api.Annotations{Name: existingSecretName},
			Driver:      &api.Driver{Name: secretDriver},
		},
	}
	doNotReuseSecret := &api.Secret{
		Id: "driverDoNotReuseSecret",
		Spec: &api.SecretSpec{
			Annotations: &api.Annotations{Name: doNotReuseExistingSecretName},
			Driver:      &api.Driver{Name: secretDriver},
		},
	}
	errSecret := &api.Secret{
		Id: "driverErrSecret",
		Spec: &api.SecretSpec{
			Annotations: &api.Annotations{Name: errSecretName},
			Driver:      &api.Driver{Name: secretDriver},
		},
	}
	config := &api.Config{
		Id: "config",
		Spec: &api.ConfigSpec{
			Data: []byte("config"),
		},
	}
	spec := taskSpecFromDependencies(secret, doNotReuseSecret, errSecret, config)
	spec.GetContainer().Hostname = serviceHostname
	task := &api.Task{
		NodeId:       nodeID,
		Id:           "secretTask",
		Status:       &api.TaskStatus{State: api.TaskState_READY},
		DesiredState: api.TaskState_NEW,
		Spec:         spec,
		Endpoint: &api.Endpoint{
			Spec: &api.EndpointSpec{
				Mode: serviceEndpointMode,
				Ports: []*api.PortConfig{
					{
						Name:          portConfig.Name,
						PublishedPort: portConfig.PublishedPort,
						Protocol:      api.PortConfig_Protocol(portConfig.Protocol),
						TargetPort:    portConfig.TargetPort,
						PublishMode:   api.PortConfig_PublishMode(portConfig.PublishMode),
					},
				},
			},
		},
		ServiceAnnotations: &api.Annotations{
			Name:   serviceName,
			Labels: serviceLabels,
		},
	}

	err := gd.Store.Update(func(tx store.Tx) error {
		assert.NoError(t, store.CreateSecret(tx, secret))
		assert.NoError(t, store.CreateSecret(tx, doNotReuseSecret))
		assert.NoError(t, store.CreateSecret(tx, errSecret))
		assert.NoError(t, store.CreateConfig(tx, config))
		assert.NoError(t, store.CreateTask(tx, task))
		return nil
	})
	assert.NoError(t, err)

	stream, err := gd.Clients[0].Assignments(context.Background(), &api.AssignmentsRequest{SessionId: expectedSessionID})
	assert.NoError(t, err)
	defer stream.CloseSend()

	resp, err := stream.Recv()
	assert.NoError(t, err)

	_, _, secretChanges, _ := splitChanges(resp.Changes)
	assert.Len(t, secretChanges, 2)
	for _, s := range secretChanges {
		if s.Id == "driverSecret" {
			assert.Equal(t, secretValue, s.Spec.GetData())
		} else if s.Id == "driverDoNotReuseSecret" {
			assert.Fail(t, "Secret with DoNotReuse==true should not retain its original ID in the assignment", "%s != %s", "driverDoNotReuseSecret", s.Id)
		} else {
			taskSpecificID := fmt.Sprintf("%s.%s", "driverDoNotReuseSecret", task.Id)
			assert.Equal(t, taskSpecificID, s.Id)
			assert.Equal(t, doNotReuseSecretValue, s.Spec.GetData())
		}
	}
}

// TestAssignmentsWithVolume tests that Assignments correctly sends down
// volumes.
func TestAssignmentsWithVolume(t *testing.T) {
	gd := startDispatcher(t, DefaultConfig())
	defer gd.Close()

	expectedSessionID, nodeID := getSessionAndNodeID(t, gd.Clients[0])

	volumes := []*api.Volume{
		{
			Id: "volumeID0",
			Spec: &api.VolumeSpec{
				Annotations: &api.Annotations{
					Name: "volumeName",
				},
				Driver: &api.Driver{
					Name: "someDriver",
				},
				Secrets: []*api.VolumeSecret{
					{
						Key:    "volumeSecret0",
						Secret: "secret0",
					}, {
						Key:    "volumeSecret1",
						Secret: "secret1",
					},
				},
			},
			VolumeInfo: &api.VolumeInfo{
				VolumeId: "csiID0",
				VolumeContext: map[string]string{
					"volumeID": "0",
				},
			},
			PublishStatus: []*api.VolumePublishStatus{
				{
					NodeId: nodeID,
					State:  api.VolumePublishStatus_PENDING_PUBLISH,
				},
			},
		}, {
			Id: "volumeID1",
			Spec: &api.VolumeSpec{
				Annotations: &api.Annotations{
					Name: "volumeOtherName",
				},
				Group: "volumeGroup",
				Driver: &api.Driver{
					Name: "someDriver",
				},
				Secrets: []*api.VolumeSecret{
					{
						Key:    "volumeSecret0",
						Secret: "secret0",
					}, {
						Key:    "volumeSecret2",
						Secret: "secret2",
					},
				},
			},
			VolumeInfo: &api.VolumeInfo{
				VolumeId: "csiID1",
				VolumeContext: map[string]string{
					"volumeID": "1",
				},
			},
			PublishStatus: []*api.VolumePublishStatus{
				{
					NodeId: nodeID,
					State:  api.VolumePublishStatus_PUBLISHED,
					PublishContext: map[string]string{
						"published": "yes",
						"volumeID":  "1",
					},
				},
			},
		},
	}

	secrets := []*api.Secret{
		{
			Id: "secret0",
			Spec: &api.SecretSpec{
				Annotations: &api.Annotations{
					Name: "secretName0",
				},
				Data: []byte("secret0 data"),
			},
		}, {
			Id: "secret1",
			Spec: &api.SecretSpec{
				Annotations: &api.Annotations{
					Name: "secretName1",
				},
				Data: []byte("secret1 data"),
			},
		}, {
			Id: "secret2",
			Spec: &api.SecretSpec{
				Annotations: &api.Annotations{
					Name: "secretName2",
				},
				Data: []byte("secret2 data"),
			},
		},
	}

	task := &api.Task{
		Id:     "task1",
		NodeId: nodeID,
		Status: &api.TaskStatus{
			State: api.TaskState_ASSIGNED,
		},
		DesiredState: api.TaskState_RUNNING,
		Spec: &api.TaskSpec{
			Runtime: &api.TaskSpec_Container{
				Container: &api.ContainerSpec{
					Mounts: []*api.Mount{
						{
							Type:   api.Mount_CLUSTER,
							Source: "volumeName",
							Target: "/foo",
						}, {
							Type:   api.Mount_CLUSTER,
							Source: "group:volumeGroup",
							Target: "/bar",
						},
					},
					Secrets: []*api.SecretReference{
						{
							SecretId:   "secret1",
							SecretName: "secretName1",
							Target: &api.SecretReference_File{
								File: &api.FileTarget{
									Name: "somefile",
								},
							},
						},
					},
				},
			},
			ResourceReferences: []*api.ResourceReference{
				{
					ResourceId:   "secret1",
					ResourceType: api.ResourceType_SECRET,
				},
			},
		},
		Volumes: []*api.VolumeAttachment{
			{
				Id:     "volumeID0",
				Source: "volumeName",
				Target: "/foo",
			}, {
				Id:     "volumeID1",
				Source: "group:volumeGroup",
				Target: "/bar",
			},
		},
	}

	err := gd.Store.Update(func(tx store.Tx) error {
		for _, secret := range secrets {
			if err := store.CreateSecret(tx, secret); err != nil {
				return err
			}
		}

		for _, volume := range volumes {
			if err := store.CreateVolume(tx, volume); err != nil {
				return err
			}
		}

		return store.CreateTask(tx, task)
	})
	assert.NoError(t, err)

	stream, err := gd.Clients[0].Assignments(
		context.Background(),
		&api.AssignmentsRequest{SessionId: expectedSessionID},
	)
	assert.NoError(t, err)
	defer stream.CloseSend()

	time.Sleep(100 * time.Millisecond)

	resp, err := stream.Recv()
	assert.NoError(t, err)

	verifyChanges(t, resp.Changes, []changeExpectations{
		{
			action:  api.AssignmentChange_UPDATE,
			tasks:   []*api.Task{task},
			secrets: secrets,
			volumes: []*api.VolumeAssignment{
				{
					Id:       "volumeID1",
					VolumeId: "csiID1",
					Driver: &api.Driver{
						Name: "someDriver",
					},
					VolumeContext: map[string]string{
						"volumeID": "1",
					},
					PublishContext: map[string]string{
						"published": "yes",
						"volumeID":  "1",
					},
					Secrets: []*api.VolumeSecret{
						{
							Key:    "volumeSecret0",
							Secret: "secret0",
						}, {
							Key:    "volumeSecret2",
							Secret: "secret2",
						},
					},
				},
			},
		},
	})

	// now update the volume to be published
	assert.NoError(t, gd.Store.Update(func(tx store.Tx) error {
		v := store.GetVolume(tx, "volumeID0")
		v.PublishStatus[0].State = api.VolumePublishStatus_PUBLISHED
		v.PublishStatus[0].PublishContext = map[string]string{
			"published": "yes",
			"volumeID":  "0",
		}
		return store.UpdateVolume(tx, v)
	}))

	// now see if we get a volume assignment
	resp, err = stream.Recv()
	assert.NoError(t, err)

	_, _, _, volumeChanges := splitChanges(resp.Changes)
	assert.Len(t, volumeChanges, 1)
	assert.Equal(t,
		volumeChanges[idAndAction{
			id:     "volumeID0",
			action: api.AssignmentChange_UPDATE,
		}],
		&api.VolumeAssignment{
			Id:       "volumeID0",
			VolumeId: "csiID0",
			Driver: &api.Driver{
				Name: "someDriver",
			},
			VolumeContext: map[string]string{
				"volumeID": "0",
			},
			PublishContext: map[string]string{
				"published": "yes",
				"volumeID":  "0",
			},
			Secrets: []*api.VolumeSecret{
				{
					Key:    "volumeSecret0",
					Secret: "secret0",
				}, {
					Key:    "volumeSecret1",
					Secret: "secret1",
				},
			},
		},
	)
}

// When connecting to a dispatcher to get Assignments, if there are tasks already in the store,
// Assignments will send down any existing node tasks > ASSIGNED, and any secrets
// for said tasks that are <= RUNNING (if the secrets exist)
func TestAssignmentsInitialNodeTasks(t *testing.T) {
	t.Parallel()
	testFuncs := []taskGeneratorFunc{
		makeTasksAndDependenciesWithResourceReferences,
		makeTasksAndDependenciesNoResourceReferences,
		makeTasksAndDependenciesOnlyResourceReferences,
		makeTasksAndDependenciesWithRedundantReferences,
	}
	for _, testFunc := range testFuncs {
		testAssignmentsInitialNodeTasksWithGivenTasks(t, testFunc)
	}
}

func testAssignmentsInitialNodeTasksWithGivenTasks(t *testing.T, genTasks taskGeneratorFunc) {
	gd := startDispatcher(t, DefaultConfig())
	defer gd.Close()

	expectedSessionID, nodeID := getSessionAndNodeID(t, gd.Clients[0])

	// create the relevant secrets and tasks
	secrets, configs, resourceRefs, tasks := genTasks(t, nodeID)
	err := gd.Store.Update(func(tx store.Tx) error {
		for _, secret := range secrets {
			assert.NoError(t, store.CreateSecret(tx, secret))
		}
		for _, config := range configs {
			assert.NoError(t, store.CreateConfig(tx, config))
		}
		// make dummy secrets and configs for resourceRefs
		for _, resourceRef := range resourceRefs {
			assert.NoError(t, makeMockResource(tx, resourceRef))
		}

		for _, task := range tasks {
			assert.NoError(t, store.CreateTask(tx, task))
		}
		return nil
	})
	assert.NoError(t, err)

	stream, err := gd.Clients[0].Assignments(context.Background(), &api.AssignmentsRequest{SessionId: expectedSessionID})
	assert.NoError(t, err)
	defer stream.CloseSend()

	time.Sleep(100 * time.Millisecond)

	// check the initial task and secret stream
	resp, err := stream.Recv()
	assert.NoError(t, err)

	assignedToRunningTasks := filterTasks(tasks, func(s api.TaskState) bool {
		return s >= api.TaskState_ASSIGNED && s <= api.TaskState_RUNNING
	})
	pastRunningTasks := filterTasks(tasks, func(s api.TaskState) bool {
		return s > api.TaskState_RUNNING
	})
	atLeastAssignedTasks := filterTasks(tasks, func(s api.TaskState) bool {
		return s >= api.TaskState_ASSIGNED
	})

	// dispatcher sends dependencies for all tasks >= ASSIGNED and <= RUNNING
	referencedSecrets, referencedConfigs := getResourcesFromReferences(gd, resourceRefs)
	secrets = append(secrets, referencedSecrets...)
	configs = append(configs, referencedConfigs...)
	updatedSecrets, updatedConfigs := filterDependencies(secrets, configs, assignedToRunningTasks, nil)
	verifyChanges(t, resp.Changes, []changeExpectations{
		{
			action:  api.AssignmentChange_UPDATE,
			tasks:   atLeastAssignedTasks, // dispatcher sends task updates for all tasks >= ASSIGNED
			secrets: updatedSecrets,
			configs: updatedConfigs,
		},
	})

	// updating all the tasks will attempt to remove all the secrets for the tasks that are in state > running
	err = gd.Store.Update(func(tx store.Tx) error {
		for _, task := range tasks {
			assert.NoError(t, store.UpdateTask(tx, task))
		}
		return nil

	})
	assert.NoError(t, err)

	resp, err = stream.Recv()
	assert.NoError(t, err)

	// dependencies for tasks > RUNNING are removed, but only if they are not currently being used
	// by a task >= ASSIGNED and <= RUNNING
	updatedSecrets, updatedConfigs = filterDependencies(secrets, configs, pastRunningTasks, assignedToRunningTasks)
	verifyChanges(t, resp.Changes, []changeExpectations{
		{
			// ASSIGNED tasks are always sent down even if they haven't changed
			action: api.AssignmentChange_UPDATE,
			tasks:  filterTasks(tasks, func(s api.TaskState) bool { return s == api.TaskState_ASSIGNED }),
		},
		{
			action:  api.AssignmentChange_REMOVE,
			secrets: updatedSecrets,
			configs: updatedConfigs,
		},
	})

	// deleting the tasks removes all the secrets for every single task, no matter
	// what state it's in
	err = gd.Store.Update(func(tx store.Tx) error {
		for _, task := range tasks {
			assert.NoError(t, store.DeleteTask(tx, task.Id))
		}
		return nil
	})
	assert.NoError(t, err)

	resp, err = stream.Recv()
	assert.NoError(t, err)

	// tasks >= ASSIGNED and their dependencies have all been removed;
	// task < ASSIGNED and their dependencies were never sent in the first place, so don't need to be removed
	updatedSecrets, updatedConfigs = filterDependencies(secrets, configs, atLeastAssignedTasks, nil)
	verifyChanges(t, resp.Changes, []changeExpectations{
		{
			action:  api.AssignmentChange_REMOVE,
			tasks:   atLeastAssignedTasks,
			secrets: updatedSecrets,
			configs: updatedConfigs,
		},
	})
}

func mockNumberedConfig(i int) *api.Config {
	return &api.Config{
		Id: fmt.Sprintf("IDconfig%d", i),
		Spec: &api.ConfigSpec{
			Annotations: &api.Annotations{
				Name: fmt.Sprintf("config%d", i),
			},
			Data: fmt.Appendf(nil, "config%d", i),
		},
	}
}

func mockNumberedSecret(i int) *api.Secret {
	return &api.Secret{
		Id: fmt.Sprintf("IDsecret%d", i),
		Spec: &api.SecretSpec{
			Annotations: &api.Annotations{
				Name: fmt.Sprintf("secret%d", i),
			},
			Data: fmt.Appendf(nil, "secret%d", i),
		},
	}
}

func mockNumberedReadyTask(i int, nodeID string, taskState api.TaskState, spec *api.TaskSpec) *api.Task {
	return &api.Task{
		NodeId:       nodeID,
		Id:           fmt.Sprintf("testTask%d", i),
		Status:       &api.TaskStatus{State: taskState},
		DesiredState: api.TaskState_READY,
		Spec:         spec,
	}
}

func makeMockResource(tx store.Tx, resourceRef *api.ResourceReference) error {
	switch resourceRef.ResourceType {
	case api.ResourceType_SECRET:
		dummySecret := &api.Secret{
			Id: resourceRef.ResourceId,
			Spec: &api.SecretSpec{
				Annotations: &api.Annotations{
					Name: fmt.Sprintf("dummy_secret_%s", resourceRef.ResourceId),
				},
				Data: fmt.Appendf(nil, "secret_%s", resourceRef.ResourceId),
			},
		}
		if store.GetSecret(tx, dummySecret.Id) == nil {
			return store.CreateSecret(tx, dummySecret)
		}
		// the resource already exists
		return nil
	case api.ResourceType_CONFIG:
		dummyConfig := &api.Config{
			Id: resourceRef.ResourceId,
			Spec: &api.ConfigSpec{
				Annotations: &api.Annotations{
					Name: fmt.Sprintf("dummy_config_%s", resourceRef.ResourceId),
				},
				Data: fmt.Appendf(nil, "config_%s", resourceRef.ResourceId),
			},
		}
		if store.GetConfig(tx, dummyConfig.Id) == nil {
			return store.CreateConfig(tx, dummyConfig)
		}
		// the resource already exists
		return nil
	default:
		return fmt.Errorf("unsupported mock resource type")
	}
}

// When connecting to a dispatcher with no tasks or assignments, when tasks are updated, assignments will send down
// tasks > ASSIGNED, and any secrets for said tasks that are <= RUNNING (but only if the secrets/configs exist - if
// they don't, even if they are referenced, the task is still sent down)
func TestAssignmentsAddingTasks(t *testing.T) {
	t.Parallel()
	testFuncs := []taskGeneratorFunc{
		makeTasksAndDependenciesWithResourceReferences,
		makeTasksAndDependenciesNoResourceReferences,
		makeTasksAndDependenciesOnlyResourceReferences,
		makeTasksAndDependenciesWithRedundantReferences,
	}
	for _, testFunc := range testFuncs {
		testAssignmentsAddingTasksWithGivenTasks(t, testFunc)
	}
}

func testAssignmentsAddingTasksWithGivenTasks(t *testing.T, genTasks taskGeneratorFunc) {
	gd := startDispatcher(t, DefaultConfig())
	defer gd.Close()

	expectedSessionID, nodeID := getSessionAndNodeID(t, gd.Clients[0])

	stream, err := gd.Clients[0].Assignments(context.Background(), &api.AssignmentsRequest{SessionId: expectedSessionID})
	assert.NoError(t, err)
	defer stream.CloseSend()

	time.Sleep(100 * time.Millisecond)

	// There are no initial tasks or secrets
	resp, err := stream.Recv()
	assert.NoError(t, err)
	assert.Empty(t, resp.Changes)

	// create the relevant secrets, configs, and tasks and update the tasks
	secrets, configs, resourceRefs, tasks := genTasks(t, nodeID)
	var createdSecrets []*api.Secret
	var createdConfigs []*api.Config
	if len(secrets) > 0 {
		createdSecrets = secrets[:len(secrets)-1]
	}
	if len(configs) > 0 {
		createdConfigs = configs[:len(configs)-1]
	}
	err = gd.Store.Update(func(tx store.Tx) error {
		for _, secret := range createdSecrets {
			if store.GetSecret(tx, secret.Id) == nil {
				assert.NoError(t, store.CreateSecret(tx, secret))
			}
		}
		for _, config := range createdConfigs {
			if store.GetConfig(tx, config.Id) == nil {
				assert.NoError(t, store.CreateConfig(tx, config))
			}
		}
		// make dummy secrets and configs for resourceRefs
		for _, resourceRef := range resourceRefs {
			assert.NoError(t, makeMockResource(tx, resourceRef))
		}

		for _, task := range tasks {
			assert.NoError(t, store.CreateTask(tx, task))
		}
		return nil
	})
	assert.NoError(t, err)

	// Nothing happens until we update.  Updating all the tasks will send updates for all the tasks >= ASSIGNED,
	// and secrets for all the tasks >= ASSIGNED and <= RUNNING.
	err = gd.Store.Update(func(tx store.Tx) error {
		for _, task := range tasks {
			assert.NoError(t, store.UpdateTask(tx, task))
		}
		return nil

	})
	assert.NoError(t, err)

	resp, err = stream.Recv()
	assert.NoError(t, err)

	assignedToRunningTasks := filterTasks(tasks, func(s api.TaskState) bool {
		return s >= api.TaskState_ASSIGNED && s <= api.TaskState_RUNNING
	})
	atLeastAssignedTasks := filterTasks(tasks, func(s api.TaskState) bool {
		return s >= api.TaskState_ASSIGNED
	})

	// dispatcher sends dependencies for all tasks >= ASSIGNED and <= RUNNING, but only if they exist in
	// the store - if a dependency is referenced by a task but does not exist, that's fine, it just won't be
	// included in the changes
	referencedSecrets, referencedConfigs := getResourcesFromReferences(gd, resourceRefs)
	createdSecrets = append(createdSecrets, referencedSecrets...)
	createdConfigs = append(createdConfigs, referencedConfigs...)
	updatedSecrets, updatedConfigs := filterDependencies(createdSecrets, createdConfigs, assignedToRunningTasks, nil)
	verifyChanges(t, resp.Changes, []changeExpectations{
		{
			action:  api.AssignmentChange_UPDATE,
			tasks:   atLeastAssignedTasks, // dispatcher sends task updates for all tasks >= ASSIGNED
			secrets: updatedSecrets,
			configs: updatedConfigs,
		},
	})

	// deleting the tasks removes all the secrets for every single task, no matter
	// what state it's in
	err = gd.Store.Update(func(tx store.Tx) error {
		for _, task := range tasks {
			assert.NoError(t, store.DeleteTask(tx, task.Id))
		}
		return nil

	})
	assert.NoError(t, err)

	resp, err = stream.Recv()
	assert.NoError(t, err)

	// tasks >= ASSIGNED and their dependencies have all been removed, even if they don't exist in the store;
	// task < ASSIGNED and their dependencies were never sent in the first place, so don't need to be removed
	secrets = append(secrets, referencedSecrets...)
	configs = append(configs, referencedConfigs...)
	updatedSecrets, updatedConfigs = filterDependencies(secrets, configs, atLeastAssignedTasks, nil)
	verifyChanges(t, resp.Changes, []changeExpectations{
		{
			action:  api.AssignmentChange_REMOVE,
			tasks:   atLeastAssignedTasks,
			secrets: updatedSecrets,
			configs: updatedConfigs,
		},
	})
}

// If a secret or config is updated or deleted, even if it's for an existing task, no changes will be sent down
func TestAssignmentsDependencyUpdateAndDeletion(t *testing.T) {
	t.Parallel()
	testFuncs := []taskGeneratorFunc{
		makeTasksAndDependenciesWithResourceReferences,
		makeTasksAndDependenciesNoResourceReferences,
		makeTasksAndDependenciesOnlyResourceReferences,
		makeTasksAndDependenciesWithRedundantReferences,
	}
	for _, testFunc := range testFuncs {
		testAssignmentsDependencyUpdateAndDeletionWithGivenTasks(t, testFunc)
	}
}

func testAssignmentsDependencyUpdateAndDeletionWithGivenTasks(t *testing.T, genTasks taskGeneratorFunc) {
	gd := startDispatcher(t, DefaultConfig())
	defer gd.Close()

	expectedSessionID, nodeID := getSessionAndNodeID(t, gd.Clients[0])

	// create the relevant secrets and tasks
	secrets, configs, resourceRefs, tasks := genTasks(t, nodeID)
	err := gd.Store.Update(func(tx store.Tx) error {
		for _, secret := range secrets {
			if store.GetSecret(tx, secret.Id) == nil {
				assert.NoError(t, store.CreateSecret(tx, secret))
			}
		}
		for _, config := range configs {
			if store.GetConfig(tx, config.Id) == nil {
				assert.NoError(t, store.CreateConfig(tx, config))
			}
		}
		// make dummy secrets and configs for resourceRefs
		for _, resourceRef := range resourceRefs {
			assert.NoError(t, makeMockResource(tx, resourceRef))
		}

		for _, task := range tasks {
			assert.NoError(t, store.CreateTask(tx, task))
		}
		return nil
	})
	assert.NoError(t, err)

	stream, err := gd.Clients[0].Assignments(context.Background(), &api.AssignmentsRequest{SessionId: expectedSessionID})
	assert.NoError(t, err)
	defer stream.CloseSend()

	time.Sleep(100 * time.Millisecond)

	// check the initial task and secret stream
	resp, err := stream.Recv()
	assert.NoError(t, err)

	assignedToRunningTasks := filterTasks(tasks, func(s api.TaskState) bool {
		return s >= api.TaskState_ASSIGNED && s <= api.TaskState_RUNNING
	})
	atLeastAssignedTasks := filterTasks(tasks, func(s api.TaskState) bool {
		return s >= api.TaskState_ASSIGNED
	})

	// dispatcher sends dependencies for all tasks >= ASSIGNED and <= RUNNING
	referencedSecrets, referencedConfigs := getResourcesFromReferences(gd, resourceRefs)
	secrets = append(secrets, referencedSecrets...)
	configs = append(configs, referencedConfigs...)
	updatedSecrets, updatedConfigs := filterDependencies(secrets, configs, assignedToRunningTasks, nil)
	verifyChanges(t, resp.Changes, []changeExpectations{
		{
			action:  api.AssignmentChange_UPDATE,
			tasks:   atLeastAssignedTasks, // dispatcher sends task updates for all tasks >= ASSIGNED
			secrets: updatedSecrets,
			configs: updatedConfigs,
		},
	})

	// updating secrets and configs, used by tasks or not, do not cause any changes
	uniqueSecrets := uniquifySecrets(secrets)
	uniqueConfigs := uniquifyConfigs(configs)
	assert.NoError(t, gd.Store.Update(func(tx store.Tx) error {
		for _, s := range uniqueSecrets {
			s.Spec.Data = []byte("new secret data")
			if err := store.UpdateSecret(tx, s); err != nil {
				return err
			}
		}
		for _, c := range uniqueConfigs {
			c.Spec.Data = []byte("new config data")
			if err := store.UpdateConfig(tx, c); err != nil {
				return err
			}
		}
		return nil
	}))

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

	// deleting secrets and configs, used by tasks or not, do not cause any changes
	err = gd.Store.Update(func(tx store.Tx) error {
		for _, secret := range uniqueSecrets {
			assert.NoError(t, store.DeleteSecret(tx, secret.Id))
		}
		for _, config := range uniqueConfigs {
			assert.NoError(t, store.DeleteConfig(tx, config.Id))
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

	gd := startDispatcher(t, DefaultConfig())
	defer gd.Close()

	var expectedSessionID string
	var nodeID string
	{
		stream, err := gd.Clients[0].Session(context.Background(), &api.SessionRequest{})
		assert.NoError(t, err)
		defer stream.CloseSend()
		resp, err := stream.Recv()
		assert.NoError(t, err)
		assert.NotEmpty(t, resp.SessionId)
		expectedSessionID = resp.SessionId
		nodeID = resp.Node.Id
	}

	testTask1 := &api.Task{
		NodeId:       nodeID,
		Id:           "testTask1",
		Status:       &api.TaskStatus{State: api.TaskState_ASSIGNED},
		DesiredState: api.TaskState_READY,
	}
	testTask2 := &api.Task{
		NodeId:       nodeID,
		Id:           "testTask2",
		Status:       &api.TaskStatus{State: api.TaskState_ASSIGNED},
		DesiredState: api.TaskState_READY,
	}

	stream, err := gd.Clients[0].Assignments(context.Background(), &api.AssignmentsRequest{SessionId: expectedSessionID})
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

	verifyChanges(t, resp.Changes, []changeExpectations{
		{
			action: api.AssignmentChange_UPDATE,
			tasks:  []*api.Task{testTask1, testTask2},
		},
	})

	assert.NoError(t, gd.Store.Update(func(tx store.Tx) error {
		task := store.GetTask(tx, testTask1.Id)
		if task == nil {
			return errors.New("no task")
		}
		task.NodeId = nodeID
		// only Status is changed for task1
		task.Status = &api.TaskStatus{State: api.TaskState_FAILED, Err: "1234"}
		task.DesiredState = api.TaskState_READY
		return store.UpdateTask(tx, task)
	}))

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
	gd := startDispatcher(t, DefaultConfig())
	defer gd.Close()

	var expectedSessionID string
	var nodeID string
	{
		stream, err := gd.Clients[0].Session(context.Background(), &api.SessionRequest{})
		assert.NoError(t, err)
		defer stream.CloseSend()
		resp, err := stream.Recv()
		assert.NoError(t, err)
		assert.NotEmpty(t, resp.SessionId)
		expectedSessionID = resp.SessionId
		nodeID = resp.Node.Id
	}

	testTask1 := &api.Task{
		NodeId: nodeID,
		Id:     "testTask1",
		Status: &api.TaskStatus{State: api.TaskState_ASSIGNED},
	}
	testTask2 := &api.Task{
		NodeId: nodeID,
		Id:     "testTask2",
		Status: &api.TaskStatus{State: api.TaskState_ASSIGNED},
	}

	stream, err := gd.Clients[0].Assignments(context.Background(), &api.AssignmentsRequest{SessionId: expectedSessionID})
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
		assert.NoError(t, store.DeleteTask(tx, testTask1.Id))
		assert.NoError(t, store.DeleteTask(tx, testTask2.Id))
		return nil
	})
	assert.NoError(t, err)

	resp, err = stream.Recv()
	assert.NoError(t, err)

	// all tasks have been deleted
	verifyChanges(t, resp.Changes, []changeExpectations{
		{
			action: api.AssignmentChange_REMOVE,
			tasks:  []*api.Task{testTask1, testTask2},
		},
	})
}

func TestTasksNoCert(t *testing.T) {
	gd := startDispatcher(t, DefaultConfig())
	defer gd.Close()

	stream, err := gd.Clients[2].Assignments(context.Background(), &api.AssignmentsRequest{})
	assert.NoError(t, err)
	assert.NotNil(t, stream)
	resp, err := stream.Recv()
	assert.Nil(t, resp)
	assert.EqualError(t, err, "rpc error: code = PermissionDenied desc = Permission denied: unauthorized peer role: rpc error: code = PermissionDenied desc = no client certificates in request")
}

func TestTaskUpdate(t *testing.T) {
	gd := startDispatcher(t, DefaultConfig())
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
		assert.NotEmpty(t, resp.SessionId)
		expectedSessionID = resp.SessionId
		nodeID = resp.Node.Id

	}
	// testTask1 and testTask2 are advanced from NEW to ASSIGNED.
	testTask1 := &api.Task{
		Id:     "testTask1",
		NodeId: nodeID,
	}
	testTask2 := &api.Task{
		Id:     "testTask2",
		NodeId: nodeID,
	}
	// testTask3 is used to confirm that status updates for a task not
	// assigned to the node sending the update are rejected.
	testTask3 := &api.Task{
		Id:     "testTask3",
		NodeId: "differentnode",
	}
	// testTask4 is used to confirm that a task's state is not allowed to
	// move backwards.
	testTask4 := &api.Task{
		Id:     "testTask4",
		NodeId: nodeID,
		Status: &api.TaskStatus{
			State: api.TaskState_SHUTDOWN,
		},
	}
	err := gd.Store.Update(func(tx store.Tx) error {
		assert.NoError(t, store.CreateTask(tx, testTask1))
		assert.NoError(t, store.CreateTask(tx, testTask2))
		assert.NoError(t, store.CreateTask(tx, testTask3))
		assert.NoError(t, store.CreateTask(tx, testTask4))
		return nil
	})
	assert.NoError(t, err)

	testTask1.Status = &api.TaskStatus{State: api.TaskState_ASSIGNED}
	testTask2.Status = &api.TaskStatus{State: api.TaskState_ASSIGNED}
	testTask3.Status = &api.TaskStatus{State: api.TaskState_ASSIGNED}
	testTask4.Status = &api.TaskStatus{State: api.TaskState_RUNNING}
	updReq := &api.UpdateTaskStatusRequest{
		Updates: []*api.UpdateTaskStatusRequest_TaskStatusUpdate{
			{
				TaskId: testTask1.Id,
				Status: testTask1.Status,
			},
			{
				TaskId: testTask2.Id,
				Status: testTask2.Status,
			},
			{
				TaskId: testTask4.Id,
				Status: testTask4.Status,
			},
		},
	}

	{
		// without correct SessionID should fail
		resp, err := gd.Clients[0].UpdateTaskStatus(context.Background(), updReq)
		assert.Nil(t, resp)
		assert.Error(t, err)
		assert.Equal(t, testutils.ErrorCode(err), codes.InvalidArgument)
	}

	updReq.SessionId = expectedSessionID
	_, err = gd.Clients[0].UpdateTaskStatus(context.Background(), updReq)
	assert.NoError(t, err)

	{
		// updating a task not assigned to us should fail
		updReq.Updates = []*api.UpdateTaskStatusRequest_TaskStatusUpdate{
			{
				TaskId: testTask3.Id,
				Status: testTask3.Status,
			},
		}

		resp, err := gd.Clients[0].UpdateTaskStatus(context.Background(), updReq)
		assert.Nil(t, resp)
		assert.Error(t, err)
		assert.Equal(t, testutils.ErrorCode(err), codes.PermissionDenied)
	}

	gd.dispatcherServer.processUpdates(context.Background())

	gd.Store.View(func(readTx store.ReadTx) {
		storeTask1 := store.GetTask(readTx, testTask1.Id)
		assert.NotNil(t, storeTask1)
		storeTask2 := store.GetTask(readTx, testTask2.Id)
		assert.NotNil(t, storeTask2)
		assert.Equal(t, storeTask1.Status.GetState(), api.TaskState_ASSIGNED)
		assert.Equal(t, storeTask2.Status.GetState(), api.TaskState_ASSIGNED)

		storeTask3 := store.GetTask(readTx, testTask3.Id)
		assert.NotNil(t, storeTask3)
		assert.Equal(t, storeTask3.Status.GetState(), api.TaskState_NEW)

		// The update to task4's state should be ignored because it
		// would have moved backwards.
		storeTask4 := store.GetTask(readTx, testTask4.Id)
		assert.NotNil(t, storeTask4)
		assert.Equal(t, storeTask4.Status.GetState(), api.TaskState_SHUTDOWN)
	})

}

func TestTaskUpdateNoCert(t *testing.T) {
	gd := startDispatcher(t, DefaultConfig())
	defer gd.Close()

	testTask1 := &api.Task{
		Id: "testTask1",
	}
	err := gd.Store.Update(func(tx store.Tx) error {
		assert.NoError(t, store.CreateTask(tx, testTask1))
		return nil
	})
	assert.NoError(t, err)

	testTask1.Status = &api.TaskStatus{State: api.TaskState_ASSIGNED}
	updReq := &api.UpdateTaskStatusRequest{
		Updates: []*api.UpdateTaskStatusRequest_TaskStatusUpdate{
			{
				TaskId: testTask1.Id,
				Status: testTask1.Status,
			},
		},
	}
	// without correct SessionID should fail
	resp, err := gd.Clients[2].UpdateTaskStatus(context.Background(), updReq)
	assert.Nil(t, resp)
	assert.Error(t, err)
	assert.EqualError(t, err, "rpc error: code = PermissionDenied desc = Permission denied: unauthorized peer role: rpc error: code = PermissionDenied desc = no client certificates in request")
}

func TestSession(t *testing.T) {
	gd := startDispatcher(t, DefaultConfig())
	defer gd.Close()

	// update the cluster to include some csi plugins
	err := gd.Store.Update(func(tx store.Tx) error {
		cluster := store.GetCluster(tx, gd.testCA.Organization)
		if cluster == nil {
			return errors.New("no cluster")
		}
		return store.UpdateCluster(tx, cluster)
	})
	require.NoError(t, err)

	stream, err := gd.Clients[0].Session(context.Background(), &api.SessionRequest{})
	assert.NoError(t, err)
	stream.CloseSend()
	resp, err := stream.Recv()
	assert.NoError(t, err)
	assert.NotEmpty(t, resp.SessionId)
	assert.Equal(t, 1, len(resp.Managers))
}

func TestSessionNoCert(t *testing.T) {
	gd := startDispatcher(t, DefaultConfig())
	defer gd.Close()

	stream, err := gd.Clients[2].Session(context.Background(), &api.SessionRequest{})
	assert.NoError(t, err)
	msg, err := stream.Recv()
	assert.Nil(t, msg)
	assert.EqualError(t, err, "rpc error: code = PermissionDenied desc = Permission denied: unauthorized peer role: rpc error: code = PermissionDenied desc = no client certificates in request")
}

func getSessionAndNodeID(t *testing.T, c api.DispatcherClient) (string, string) {
	stream, err := c.Session(context.Background(), &api.SessionRequest{})
	assert.NoError(t, err)
	defer stream.CloseSend()
	resp, err := stream.Recv()
	assert.NoError(t, err)
	assert.NotEmpty(t, resp.SessionId)
	return resp.SessionId, resp.Node.Id
}

type idAndAction struct {
	id     string
	action api.AssignmentChange_AssignmentAction
}

func splitChanges(changes []*api.AssignmentChange) (map[idAndAction]*api.Task, map[idAndAction]*api.Config, map[idAndAction]*api.Secret, map[idAndAction]*api.VolumeAssignment) {
	tasks := make(map[idAndAction]*api.Task)
	secrets := make(map[idAndAction]*api.Secret)
	configs := make(map[idAndAction]*api.Config)
	volumes := make(map[idAndAction]*api.VolumeAssignment)
	for _, change := range changes {
		task := change.Assignment.GetTask()
		if task != nil {
			tasks[idAndAction{id: task.Id, action: change.Action}] = task
		}
		secret := change.Assignment.GetSecret()
		if secret != nil {
			secrets[idAndAction{id: secret.Id, action: change.Action}] = secret
		}
		config := change.Assignment.GetConfig()
		if config != nil {
			configs[idAndAction{id: config.Id, action: change.Action}] = config
		}
		volume := change.Assignment.GetVolume()
		if volume != nil {
			volumes[idAndAction{id: volume.Id, action: change.Action}] = volume
		}
	}

	return tasks, configs, secrets, volumes
}

type changeExpectations struct {
	tasks   []*api.Task
	secrets []*api.Secret
	configs []*api.Config
	volumes []*api.VolumeAssignment
	action  api.AssignmentChange_AssignmentAction
}

// Ensures that the changes contain the following actions for the following tasks/secrets/configs
func verifyChanges(t *testing.T, changes []*api.AssignmentChange, expectations []changeExpectations) {
	taskChanges, configChanges, secretChanges, volumeChanges := splitChanges(changes)

	var expectedTasks, expectedSecrets, expectedConfigs, expectedVolumes int
	for _, c := range expectations {
		for _, task := range c.tasks {
			expectedTasks++
			index := idAndAction{id: task.Id, action: c.action}
			require.NotNil(t, taskChanges[index], "missing task change %v", index)
		}

		for _, secret := range c.secrets {
			expectedSecrets++
			index := idAndAction{id: secret.Id, action: c.action}
			require.NotNil(t, secretChanges[index], "missing secret change %v", index)
		}

		for _, config := range c.configs {
			expectedConfigs++
			index := idAndAction{id: config.Id, action: c.action}
			require.NotNil(t, configChanges[index], "missing config change %v", index)
		}
		for _, volume := range c.volumes {
			expectedVolumes++
			index := idAndAction{id: volume.Id, action: c.action}
			require.NotNil(t, volumeChanges[index], "missing volume change %v", index)
		}
	}

	require.Len(t, taskChanges, expectedTasks)
	require.Len(t, secretChanges, expectedSecrets)
	require.Len(t, configChanges, expectedConfigs)
	require.Len(t, volumeChanges, expectedVolumes)
	require.Len(t, changes, expectedTasks+expectedSecrets+expectedConfigs+expectedVolumes)
}

// filter all tasks by task state, which is given by a function because it's hard to take a range of constants
func filterTasks(tasks []*api.Task, include func(api.TaskState) bool) []*api.Task {
	var result []*api.Task
	for _, t := range tasks {
		if include(t.Status.GetState()) {
			result = append(result, t)
		}
	}
	return result
}

func getResourcesFromReferences(gd *grpcDispatcher, resourceRefs []*api.ResourceReference) ([]*api.Secret, []*api.Config) {
	var (
		referencedSecrets []*api.Secret
		referencedConfigs []*api.Config
	)
	for _, ref := range resourceRefs {
		switch ref.ResourceType {
		case api.ResourceType_SECRET:
			gd.Store.View(func(readTx store.ReadTx) {
				referencedSecrets = append(referencedSecrets, store.GetSecret(readTx, ref.ResourceId))
			})
		case api.ResourceType_CONFIG:
			gd.Store.View(func(readTx store.ReadTx) {
				referencedConfigs = append(referencedConfigs, store.GetConfig(readTx, ref.ResourceId))
			})
		}
	}
	return referencedSecrets, referencedConfigs
}

// filters all dependencies (secrets, configs); dependencies should be in inTasks, but not be in notInTasks
func filterDependencies(secrets []*api.Secret, configs []*api.Config, inTasks, notInTasks []*api.Task) ([]*api.Secret, []*api.Config) {
	var (
		wantSecrets, wantConfigs = make(map[string]struct{}), make(map[string]struct{})
		filteredSecrets          []*api.Secret
		filteredConfigs          []*api.Config
	)
	for _, t := range inTasks {
		for _, s := range t.Spec.GetContainer().GetSecrets() {
			wantSecrets[s.SecretId] = struct{}{}
		}
		for _, s := range t.Spec.GetContainer().GetConfigs() {
			wantConfigs[s.ConfigId] = struct{}{}
		}
		for _, ref := range t.Spec.GetResourceReferences() {
			switch ref.ResourceType {
			case api.ResourceType_SECRET:
				wantSecrets[ref.ResourceId] = struct{}{}
			case api.ResourceType_CONFIG:
				wantConfigs[ref.ResourceId] = struct{}{}
			}
		}
	}
	for _, t := range notInTasks {
		for _, s := range t.Spec.GetContainer().GetSecrets() {
			delete(wantSecrets, s.SecretId)
		}
		for _, s := range t.Spec.GetContainer().GetConfigs() {
			delete(wantConfigs, s.ConfigId)
		}
		for _, ref := range t.Spec.GetResourceReferences() {
			switch ref.ResourceType {
			case api.ResourceType_SECRET:
				delete(wantSecrets, ref.ResourceId)
			case api.ResourceType_CONFIG:
				delete(wantConfigs, ref.ResourceId)
			}
		}
	}
	for _, s := range secrets {
		if _, ok := wantSecrets[s.Id]; ok {
			filteredSecrets = append(filteredSecrets, s)
		}
	}
	for _, c := range configs {
		if _, ok := wantConfigs[c.Id]; ok {
			filteredConfigs = append(filteredConfigs, c)
		}
	}
	return uniquifySecrets(filteredSecrets), uniquifyConfigs(filteredConfigs)
}

func uniquifySecrets(secrets []*api.Secret) []*api.Secret {
	uniqueSecrets := make(map[string]struct{})
	var finalSecrets []*api.Secret
	for _, secret := range secrets {
		if _, ok := uniqueSecrets[secret.Id]; !ok {
			uniqueSecrets[secret.Id] = struct{}{}
			finalSecrets = append(finalSecrets, secret)
		}
	}
	return finalSecrets
}

func uniquifyConfigs(configs []*api.Config) []*api.Config {
	uniqueConfigs := make(map[string]struct{})
	var finalConfigs []*api.Config
	for _, config := range configs {
		if _, ok := uniqueConfigs[config.Id]; !ok {
			uniqueConfigs[config.Id] = struct{}{}
			finalConfigs = append(finalConfigs, config)
		}
	}
	return finalConfigs
}

type taskGeneratorFunc func(t *testing.T, nodeID string) ([]*api.Secret, []*api.Config, []*api.ResourceReference, []*api.Task)

// Creates 1 task for every possible task state, so there are 12 tasks, ID=0-11 inclusive.
// Creates 1 secret and 1 config for every single task state + 1, so there are 13 secrets, 13 configs, ID=0-12 inclusive
// Creates 1 secret and 1 config per task by resource reference so there are an additional of each eventually created
// For each task, the dependencies assigned to it are: secret, secret12, config, config12, resourceRefSecret, resourceRefConfig
func makeTasksAndDependenciesWithResourceReferences(t *testing.T, nodeID string) ([]*api.Secret, []*api.Config, []*api.ResourceReference, []*api.Task) {
	var (
		secrets      []*api.Secret
		configs      []*api.Config
		resourceRefs []*api.ResourceReference
		tasks        []*api.Task
	)
	for i := 0; i <= len(taskStatesInOrder); i++ {
		secrets = append(secrets, mockNumberedSecret(i))
		configs = append(configs, mockNumberedConfig(i))

		resourceRefs = append(resourceRefs, &api.ResourceReference{
			ResourceId:   fmt.Sprintf("IDresourceRefSecret%d", i),
			ResourceType: api.ResourceType_SECRET,
		}, &api.ResourceReference{
			ResourceId:   fmt.Sprintf("IDresourceRefConfig%d", i),
			ResourceType: api.ResourceType_CONFIG,
		})
	}

	for i, taskState := range taskStatesInOrder {
		spec := taskSpecFromDependencies(secrets[i], secrets[len(secrets)-1], configs[i], configs[len(configs)-1], resourceRefs[2*i], resourceRefs[2*i+1])
		tasks = append(tasks, mockNumberedReadyTask(i, nodeID, taskState, spec))
	}
	return secrets, configs, resourceRefs, tasks
}

// Creates 1 task for every possible task state, so there are 12 tasks, ID=0-11 inclusive.
// Creates 1 secret and 1 config for every single task state + 1, so there are 13 secrets, 13 configs, ID=0-12 inclusive
// For each task, the dependencies assigned to it are: secret<i>, secret12, config<i>, config12.
// There are no ResourceReferences in these TaskSpecs
func makeTasksAndDependenciesNoResourceReferences(t *testing.T, nodeID string) ([]*api.Secret, []*api.Config, []*api.ResourceReference, []*api.Task) {
	var (
		secrets      []*api.Secret
		configs      []*api.Config
		resourceRefs []*api.ResourceReference
		tasks        []*api.Task
	)
	for i := 0; i <= len(taskStatesInOrder); i++ {
		secrets = append(secrets, mockNumberedSecret(i))
		configs = append(configs, mockNumberedConfig(i))
	}
	for i, taskState := range taskStatesInOrder {
		spec := taskSpecFromDependencies(secrets[i], secrets[len(secrets)-1], configs[i], configs[len(configs)-1])
		tasks = append(tasks, mockNumberedReadyTask(i, nodeID, taskState, spec))
	}
	return secrets, configs, resourceRefs, tasks
}

// Creates 1 secret and 1 config per task by resource reference
// For each task, the dependencies assigned to it are: resourceRefSecret<i>, resourceRefConfig<i>,.
func makeTasksAndDependenciesOnlyResourceReferences(t *testing.T, nodeID string) ([]*api.Secret, []*api.Config, []*api.ResourceReference, []*api.Task) {
	var (
		secrets      []*api.Secret
		configs      []*api.Config
		resourceRefs []*api.ResourceReference
		tasks        []*api.Task
	)
	for i := 0; i <= len(taskStatesInOrder); i++ {
		resourceRefs = append(resourceRefs, &api.ResourceReference{
			ResourceId:   fmt.Sprintf("IDresourceRefSecret%d", i),
			ResourceType: api.ResourceType_SECRET,
		}, &api.ResourceReference{
			ResourceId:   fmt.Sprintf("IDresourceRefConfig%d", i),
			ResourceType: api.ResourceType_CONFIG,
		})
	}
	for i, taskState := range taskStatesInOrder {
		spec := taskSpecFromDependencies(resourceRefs[2*i], resourceRefs[2*i+1])
		tasks = append(tasks, mockNumberedReadyTask(i, nodeID, taskState, spec))
	}
	return secrets, configs, resourceRefs, tasks
}

// Creates 1 task for every possible task state, so there are 12 tasks, ID=0-11 inclusive.
// Creates 1 secret and 1 config for every single task state + 1, so there are 13 secrets, 13 configs, ID=0-12 inclusive
// Creates 1 secret and 1 config per task by resource reference, however they point to existing ID=0-12 secrets and configs so they are not created
// For each task, the dependencies assigned to it are: secret<i>, secret12, config<i>, config12.
func makeTasksAndDependenciesWithRedundantReferences(t *testing.T, nodeID string) ([]*api.Secret, []*api.Config, []*api.ResourceReference, []*api.Task) {
	var (
		secrets      []*api.Secret
		configs      []*api.Config
		resourceRefs []*api.ResourceReference
		tasks        []*api.Task
	)
	for i := 0; i <= len(taskStatesInOrder); i++ {
		secrets = append(secrets, mockNumberedSecret(i))
		configs = append(configs, mockNumberedConfig(i))

		// Note that the IDs here will match the original secret and config reference IDs
		resourceRefs = append(resourceRefs, &api.ResourceReference{
			ResourceId:   fmt.Sprintf("IDsecret%d", i),
			ResourceType: api.ResourceType_SECRET,
		}, &api.ResourceReference{
			ResourceId:   fmt.Sprintf("IDconfig%d", i),
			ResourceType: api.ResourceType_CONFIG,
		})
	}

	for i, taskState := range taskStatesInOrder {
		spec := taskSpecFromDependencies(secrets[i], secrets[len(secrets)-1], configs[i], configs[len(configs)-1], resourceRefs[2*i], resourceRefs[2*i+1])
		tasks = append(tasks, mockNumberedReadyTask(i, nodeID, taskState, spec))
	}
	return secrets, configs, resourceRefs, tasks
}

func taskSpecFromDependencies(dependencies ...any) *api.TaskSpec {
	var secretRefs []*api.SecretReference
	var configRefs []*api.ConfigReference
	var resourceRefs []*api.ResourceReference
	for _, d := range dependencies {
		switch v := d.(type) {
		case *api.Secret:
			secretRefs = append(secretRefs, &api.SecretReference{
				SecretName: v.GetSpec().GetAnnotations().GetName(),
				SecretId:   v.Id,
				Target: &api.SecretReference_File{
					File: &api.FileTarget{
						Name: "target.txt",
						Uid:  "0",
						Gid:  "0",
						Mode: 0666,
					},
				},
			})
		case *api.Config:
			configRefs = append(configRefs, &api.ConfigReference{
				ConfigName: v.GetSpec().GetAnnotations().GetName(),
				ConfigId:   v.Id,
				Target: &api.ConfigReference_File{
					File: &api.FileTarget{
						Name: "target.txt",
						Uid:  "0",
						Gid:  "0",
						Mode: 0666,
					},
				},
			})
		case *api.ResourceReference:
			resourceRefs = append(resourceRefs, &api.ResourceReference{
				ResourceId:   v.ResourceId,
				ResourceType: v.ResourceType,
			})
		default:
			panic("unexpected dependency type")
		}
	}
	return &api.TaskSpec{
		ResourceReferences: resourceRefs,
		Runtime: &api.TaskSpec_Container{
			Container: &api.ContainerSpec{
				Secrets: secretRefs,
				Configs: configRefs,
			},
		},
	}
}

var taskStatesInOrder = []api.TaskState{
	api.TaskState_NEW,
	api.TaskState_PENDING,
	api.TaskState_ASSIGNED,
	api.TaskState_ACCEPTED,
	api.TaskState_PREPARING,
	api.TaskState_READY,
	api.TaskState_STARTING,
	api.TaskState_RUNNING,
	api.TaskState_COMPLETE,
	api.TaskState_SHUTDOWN,
	api.TaskState_FAILED,
	api.TaskState_REJECTED,
}

// Ensure we test the old Tasks() API for backwards compat

func TestOldTasks(t *testing.T) {
	t.Parallel()

	gd := startDispatcher(t, DefaultConfig())
	defer gd.Close()

	var expectedSessionID string
	var nodeID string
	{
		stream, err := gd.Clients[0].Session(context.Background(), &api.SessionRequest{})
		assert.NoError(t, err)
		defer stream.CloseSend()
		resp, err := stream.Recv()
		assert.NoError(t, err)
		assert.NotEmpty(t, resp.SessionId)
		expectedSessionID = resp.SessionId
		nodeID = resp.Node.Id
	}

	testTask1 := &api.Task{
		NodeId:       nodeID,
		Id:           "testTask1",
		Status:       &api.TaskStatus{State: api.TaskState_ASSIGNED},
		DesiredState: api.TaskState_READY,
	}
	testTask2 := &api.Task{
		NodeId:       nodeID,
		Id:           "testTask2",
		Status:       &api.TaskStatus{State: api.TaskState_ASSIGNED},
		DesiredState: api.TaskState_READY,
	}

	{
		// without correct SessionID should fail
		stream, err := gd.Clients[0].Tasks(context.Background(), &api.TasksRequest{})
		assert.NoError(t, err)
		assert.NotNil(t, stream)
		resp, err := stream.Recv()
		assert.Nil(t, resp)
		assert.Error(t, err)
		assert.Equal(t, testutils.ErrorCode(err), codes.InvalidArgument)
	}

	stream, err := gd.Clients[0].Tasks(context.Background(), &api.TasksRequest{SessionId: expectedSessionID})
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
	assert.True(t, resp.Tasks[0].Id == "testTask1" && resp.Tasks[1].Id == "testTask2" || resp.Tasks[0].Id == "testTask2" && resp.Tasks[1].Id == "testTask1")

	assert.NoError(t, gd.Store.Update(func(tx store.Tx) error {
		task := store.GetTask(tx, testTask1.Id)
		if task == nil {
			return errors.New("no task")
		}
		task.NodeId = nodeID
		task.Status = &api.TaskStatus{State: api.TaskState_ASSIGNED}
		task.DesiredState = api.TaskState_RUNNING
		return store.UpdateTask(tx, task)
	}))

	resp, err = stream.Recv()
	assert.NoError(t, err)
	assert.Equal(t, len(resp.Tasks), 2)
	for _, task := range resp.Tasks {
		if task.Id == "testTask1" {
			assert.Equal(t, task.DesiredState, api.TaskState_RUNNING)
		}
	}

	err = gd.Store.Update(func(tx store.Tx) error {
		assert.NoError(t, store.DeleteTask(tx, testTask1.Id))
		assert.NoError(t, store.DeleteTask(tx, testTask2.Id))
		return nil
	})
	assert.NoError(t, err)

	resp, err = stream.Recv()
	assert.NoError(t, err)
	assert.Equal(t, len(resp.Tasks), 0)
}

func TestOldTasksStatusChange(t *testing.T) {
	t.Parallel()

	gd := startDispatcher(t, DefaultConfig())
	defer gd.Close()

	var expectedSessionID string
	var nodeID string
	{
		stream, err := gd.Clients[0].Session(context.Background(), &api.SessionRequest{})
		assert.NoError(t, err)
		defer stream.CloseSend()
		resp, err := stream.Recv()
		assert.NoError(t, err)
		assert.NotEmpty(t, resp.SessionId)
		expectedSessionID = resp.SessionId
		nodeID = resp.Node.Id
	}

	testTask1 := &api.Task{
		NodeId:       nodeID,
		Id:           "testTask1",
		Status:       &api.TaskStatus{State: api.TaskState_ASSIGNED},
		DesiredState: api.TaskState_READY,
	}
	testTask2 := &api.Task{
		NodeId:       nodeID,
		Id:           "testTask2",
		Status:       &api.TaskStatus{State: api.TaskState_ASSIGNED},
		DesiredState: api.TaskState_READY,
	}

	{
		// without correct SessionID should fail
		stream, err := gd.Clients[0].Tasks(context.Background(), &api.TasksRequest{})
		assert.NoError(t, err)
		assert.NotNil(t, stream)
		resp, err := stream.Recv()
		assert.Nil(t, resp)
		assert.Error(t, err)
		assert.Equal(t, testutils.ErrorCode(err), codes.InvalidArgument)
	}

	stream, err := gd.Clients[0].Tasks(context.Background(), &api.TasksRequest{SessionId: expectedSessionID})
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
	assert.True(t, resp.Tasks[0].Id == "testTask1" && resp.Tasks[1].Id == "testTask2" || resp.Tasks[0].Id == "testTask2" && resp.Tasks[1].Id == "testTask1")

	assert.NoError(t, gd.Store.Update(func(tx store.Tx) error {
		task := store.GetTask(tx, testTask1.Id)
		if task == nil {
			return errors.New("no task")
		}
		task.NodeId = nodeID
		// only Status is changed for task1
		task.Status = &api.TaskStatus{State: api.TaskState_FAILED, Err: "1234"}
		task.DesiredState = api.TaskState_READY
		return store.UpdateTask(tx, task)
	}))

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
	gd := startDispatcher(t, DefaultConfig())
	defer gd.Close()

	var expectedSessionID string
	var nodeID string
	{
		stream, err := gd.Clients[0].Session(context.Background(), &api.SessionRequest{})
		assert.NoError(t, err)
		defer stream.CloseSend()
		resp, err := stream.Recv()
		assert.NoError(t, err)
		assert.NotEmpty(t, resp.SessionId)
		expectedSessionID = resp.SessionId
		nodeID = resp.Node.Id
	}

	testTask1 := &api.Task{
		NodeId: nodeID,
		Id:     "testTask1",
		Status: &api.TaskStatus{State: api.TaskState_ASSIGNED},
	}
	testTask2 := &api.Task{
		NodeId: nodeID,
		Id:     "testTask2",
		Status: &api.TaskStatus{State: api.TaskState_ASSIGNED},
	}

	stream, err := gd.Clients[0].Tasks(context.Background(), &api.TasksRequest{SessionId: expectedSessionID})
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
		assert.NoError(t, store.DeleteTask(tx, testTask1.Id))
		assert.NoError(t, store.DeleteTask(tx, testTask2.Id))
		return nil
	})
	assert.NoError(t, err)

	resp, err = stream.Recv()
	assert.NoError(t, err)
	// all tasks have been deleted
	assert.Equal(t, len(resp.Tasks), 0)
}

func TestOldTasksNoCert(t *testing.T) {
	gd := startDispatcher(t, DefaultConfig())
	defer gd.Close()

	stream, err := gd.Clients[2].Tasks(context.Background(), &api.TasksRequest{})
	assert.NoError(t, err)
	assert.NotNil(t, stream)
	resp, err := stream.Recv()
	assert.Nil(t, resp)
	assert.EqualError(t, err, "rpc error: code = PermissionDenied desc = Permission denied: unauthorized peer role: rpc error: code = PermissionDenied desc = no client certificates in request")
}

func TestClusterUpdatesSendMessages(t *testing.T) {
	cfg := DefaultConfig()
	cfg.RateLimitPeriod = 0
	gd := startDispatcher(t, cfg)
	defer gd.Close()

	stream, err := gd.Clients[0].Session(context.Background(), &api.SessionRequest{})
	require.NoError(t, err)
	defer stream.CloseSend()

	var msg *api.SessionMessage
	{
		msg, err = stream.Recv()
		require.NoError(t, err)
		require.NotEmpty(t, msg.SessionId)
		require.NotNil(t, msg.Node)
		require.Len(t, msg.Managers, 1)
		require.Empty(t, msg.NetworkBootstrapKeys)
		require.Equal(t, gd.testCA.RootCA.Certs, msg.RootCA)
	}

	// changing the network bootstrap keys results in a new message with updated keys
	expected := msg.Copy()
	expected.NetworkBootstrapKeys = []*api.EncryptionKey{
		{Key: []byte("network key1")},
		{Key: []byte("network key2")},
	}
	require.NoError(t, gd.Store.Update(func(tx store.Tx) error {
		cluster := store.GetCluster(tx, gd.testCA.Organization)
		if cluster == nil {
			return errors.New("no cluster")
		}
		cluster.NetworkBootstrapKeys = expected.NetworkBootstrapKeys
		return store.UpdateCluster(tx, cluster)
	}))
	time.Sleep(100 * time.Millisecond)
	{
		msg, err = stream.Recv()
		require.NoError(t, err)
		require.True(t, expected.EqualVT(msg), "session message differs:\n want %v\n  got %v", expected, msg)
	}

	// changing the peers results in a new message with updated managers
	gd.testCluster.addMember("1.1.1.1")
	time.Sleep(100 * time.Millisecond)
	{
		msg, err = stream.Recv()
		require.NoError(t, err)
		require.Len(t, msg.Managers, 2)
		expected.Managers = msg.Managers
		require.True(t, expected.EqualVT(msg), "session message differs:\n want %v\n  got %v", expected, msg)
	}

	// changing the rootCA cert and has in the cluster results in a new message with an updated cert
	expected = msg.Copy()
	expected.RootCA = cautils.ECDSA256SHA256Cert
	require.NoError(t, gd.Store.Update(func(tx store.Tx) error {
		cluster := store.GetCluster(tx, gd.testCA.Organization)
		if cluster == nil {
			return errors.New("no cluster")
		}
		cluster.RootCa.CaCert = cautils.ECDSA256SHA256Cert
		cluster.RootCa.CaCertHash = digest.FromBytes(cautils.ECDSA256SHA256Cert).String()
		return store.UpdateCluster(tx, cluster)
	}))
	time.Sleep(100 * time.Millisecond)
	{
		msg, err = stream.Recv()
		require.NoError(t, err)
		require.True(t, expected.EqualVT(msg), "session message differs:\n want %v\n  got %v", expected, msg)
	}
}

// mockPluginGetter enables mocking the server plugin getter with customized plugins
type mockPluginGetter struct {
	name   string
	plugin plugin.Plugin
}

var _ plugin.Getter = &mockPluginGetter{}

// SetupPlugin setup a new plugin - the same plugin wil always return in all calls
func (m *mockPluginGetter) SetupPlugin(name string, client plugin.Client) error {
	m.plugin = NewMockPlugin(m.name, client)
	m.name = name
	return nil
}

func (m *mockPluginGetter) Get(name, capability string) (plugin.Plugin, error) {
	if name != m.name {
		return nil, fmt.Errorf("plugin with name %s not defined", name)
	}
	return m.plugin, nil
}
func (m *mockPluginGetter) GetAllManagedPluginsByCap(capability string) []plugin.Plugin {
	return nil
}

// MockPlugin mocks a v2 docker plugin
type MockPlugin struct {
	client plugin.Client
	name   string
}

// NewMockPlugin creates a new v2 plugin fake (returns the specified client and name for all calls)
func NewMockPlugin(name string, client plugin.Client) *MockPlugin {
	return &MockPlugin{name: name, client: client}
}

func (m *MockPlugin) Client() plugin.Client {
	return m.client
}
func (m *MockPlugin) Name() string {
	return m.name
}
func (m *MockPlugin) ScopedPath(_ string) string {
	return ""
}

type MockPluginHandlerFn func(argsJSON []byte) (any, error)

type MockPluginClient struct {
	handlers map[string]MockPluginHandlerFn
}

func (mc *MockPluginClient) HandleFunc(method string, fn MockPluginHandlerFn) {
	if mc.handlers == nil {
		mc.handlers = make(map[string]MockPluginHandlerFn)
	}
	if _, ok := mc.handlers[method]; ok {
		panic(fmt.Sprintf("handler for %s already exists", method))
	}
	mc.handlers[method] = fn
}

func (mc *MockPluginClient) Call(method string, args, ret any) error {
	fn, ok := mc.handlers[method]
	if !ok {
		return fmt.Errorf("no handler for %s", method)
	}
	jsonArgs, err := json.Marshal(args)
	if err != nil {
		return err
	}
	res, err := fn(jsonArgs)
	if err != nil {
		return err
	}
	jsonRes, err := json.Marshal(res)
	if err != nil {
		return fmt.Errorf("error marshalling response: %v", err)
	}
	return json.Unmarshal(jsonRes, ret)
}
