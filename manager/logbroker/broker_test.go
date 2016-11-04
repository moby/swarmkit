package logbroker

import (
	"fmt"
	"net"
	"sync"
	"testing"
	"time"

	"golang.org/x/net/context"
	"google.golang.org/grpc"

	"github.com/docker/swarmkit/api"
	"github.com/docker/swarmkit/ca"
	"github.com/docker/swarmkit/ca/testutils"
	"github.com/docker/swarmkit/protobuf/ptypes"
	"github.com/stretchr/testify/assert"
)

func TestLogBroker(t *testing.T) {
	ctx, broker, agentSecurity, client, brokerClient, done := testLogBrokerEnv(t)
	defer done()

	var (
		wg               sync.WaitGroup
		hold             = make(chan struct{}) // coordinates pubsub start
		messagesExpected int
	)

	subStream, err := brokerClient.ListenSubscriptions(ctx, &api.ListenSubscriptionsRequest{})
	if err != nil {
		t.Fatal(err)
	}

	stream, err := client.SubscribeLogs(ctx, &api.SubscribeLogsRequest{
		// Dummy selector - they are ignored in the broker for the time being.
		Selector: &api.LogSelector{
			NodeIDs: []string{"node-1"},
		},
	})
	if err != nil {
		t.Fatalf("error subscribing: %v", err)
	}

	subscription, err := subStream.Recv()
	if err != nil {
		t.Fatal(err)
	}

	// spread some services across nodes with a bunch of tasks.
	const (
		nNodes              = 5
		nServices           = 20
		nTasksPerService    = 20
		nLogMessagesPerTask = 5
	)

	for service := 0; service < nServices; service++ {
		serviceID := fmt.Sprintf("service-%v", service)

		for task := 0; task < nTasksPerService; task++ {
			taskID := fmt.Sprintf("%v.task-%v", serviceID, task)

			for node := 0; node < nNodes; node++ {
				nodeID := fmt.Sprintf("node-%v", node)

				if (task+1)%(node+1) != 0 {
					continue
				}
				messagesExpected += nLogMessagesPerTask

				wg.Add(1)
				go func(nodeID, serviceID, taskID string) {
					<-hold

					// Each goroutine gets its own publisher
					publisher, err := brokerClient.PublishLogs(ctx)
					assert.NoError(t, err)

					defer func() {
						_, err := publisher.CloseAndRecv()
						assert.NoError(t, err)
						wg.Done()
					}()

					msgctx := api.LogContext{
						NodeID:    agentSecurity.ClientTLSCreds.NodeID(),
						ServiceID: serviceID,
						TaskID:    taskID,
					}
					for i := 0; i < nLogMessagesPerTask; i++ {
						assert.NoError(t, publisher.Send(&api.PublishLogsMessage{
							SubscriptionID: subscription.ID,
							Messages:       []api.LogMessage{newLogMessage(msgctx, "log message number %d", i)},
						}))
					}
				}(nodeID, serviceID, taskID)
			}
		}
	}

	t.Logf("expected %v messages", messagesExpected)
	close(hold)
	var messages int
	for messages < messagesExpected {
		msgs, err := stream.Recv()
		assert.NoError(t, err)
		for range msgs.Messages {
			messages++
			if messages%100 == 0 {
				fmt.Println(messages, "received")
			}
		}
	}
	t.Logf("received %v messages", messages)

	wg.Wait()

	// Make sure double Run throws an error
	assert.EqualError(t, broker.Run(ctx), errAlreadyRunning.Error())
	// Stop should work
	assert.NoError(t, broker.Stop())
	// Double stopping should fail
	assert.EqualError(t, broker.Stop(), errNotRunning.Error())
}

func TestLogBrokerRegistration(t *testing.T) {
	ctx, _, _, client, brokerClient, done := testLogBrokerEnv(t)
	defer done()

	// Have an agent listen to subscriptions before anyone has subscribed.
	subscriptions1, err := brokerClient.ListenSubscriptions(ctx, &api.ListenSubscriptionsRequest{})
	assert.NoError(t, err)

	// Subscribe
	_, err = client.SubscribeLogs(ctx, &api.SubscribeLogsRequest{
		// Dummy selector - they are ignored in the broker for the time being.
		Selector: &api.LogSelector{
			NodeIDs: []string{"node-1"},
		},
	})
	assert.NoError(t, err)

	// Make sure we received the subscription with our already-connected agent.
	{
		subscription, err := subscriptions1.Recv()
		assert.NoError(t, err)
		assert.NotNil(t, subscription)
		assert.False(t, subscription.Close)
	}

	// Join a second agent.
	subscriptions2, err := brokerClient.ListenSubscriptions(ctx, &api.ListenSubscriptionsRequest{})
	assert.NoError(t, err)

	// Make sure we receive past subscriptions.
	{
		subscription, err := subscriptions2.Recv()
		assert.NoError(t, err)
		assert.NotNil(t, subscription)
		assert.False(t, subscription.Close)
	}
}

func testLogBrokerEnv(t *testing.T) (context.Context, *LogBroker, *ca.SecurityConfig, api.LogsClient, api.LogBrokerClient, func()) {
	ctx, cancel := context.WithCancel(context.Background())
	broker := New()

	tca := testutils.NewTestCA(nil)
	agentSecurityConfig, err := tca.NewNodeConfig(ca.WorkerRole)
	if err != nil {
		t.Fatal(err)
	}
	managerSecurityConfig, err := tca.NewNodeConfig(ca.ManagerRole)
	if err != nil {
		t.Fatal(err)
	}

	// Log Server
	logListener, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("error setting up listener: %v", err)
	}
	logServer := grpc.NewServer()
	api.RegisterLogsServer(logServer, broker)

	go func() {
		if err := logServer.Serve(logListener); err != nil {
			// SIGH(stevvooe): GRPC won't really shutdown gracefully.
			// This should be fatal.
			t.Logf("error serving grpc service: %v", err)
		}
	}()

	// Log Broker
	brokerListener, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("error setting up listener: %v", err)
	}

	serverOpts := []grpc.ServerOption{grpc.Creds(managerSecurityConfig.ServerTLSCreds)}

	brokerServer := grpc.NewServer(serverOpts...)

	authorize := func(ctx context.Context, roles []string) error {
		_, err := ca.AuthorizeForwardedRoleAndOrg(ctx, roles, []string{ca.ManagerRole}, tca.Organization, nil)
		return err
	}
	authenticatedLogBrokerAPI := api.NewAuthenticatedWrapperLogBrokerServer(broker, authorize)

	api.RegisterLogBrokerServer(brokerServer, authenticatedLogBrokerAPI)
	go func() {
		if err := brokerServer.Serve(brokerListener); err != nil {
			// SIGH(stevvooe): GRPC won't really shutdown gracefully.
			// This should be fatal.
			t.Logf("error serving grpc service: %v", err)
		}
	}()

	// Log client
	logCc, err := grpc.Dial(logListener.Addr().String(), grpc.WithInsecure())
	if err != nil {
		t.Fatalf("error dialing local server: %v", err)
	}
	logClient := api.NewLogsClient(logCc)

	// Broker client
	fmt.Printf("broker client: %s\n", brokerListener.Addr())
	clientOpts := []grpc.DialOption{grpc.WithTimeout(10 * time.Second), grpc.WithTransportCredentials(agentSecurityConfig.ClientTLSCreds)}
	brokerCc, err := grpc.Dial(brokerListener.Addr().String(), clientOpts...)
	if err != nil {
		t.Fatalf("error dialing local server: %v", err)
	}
	brokerClient := api.NewLogBrokerClient(brokerCc)

	go broker.Run(ctx)

	return ctx, broker, agentSecurityConfig, logClient, brokerClient, func() {
		broker.Stop()

		logCc.Close()
		brokerCc.Close()

		logServer.Stop()
		brokerServer.Stop()

		logListener.Close()
		brokerListener.Close()

		cancel()
	}
}

func printLogMessages(msgs ...api.LogMessage) {
	for _, msg := range msgs {
		ts, _ := ptypes.Timestamp(msg.Timestamp)
		fmt.Printf("%v %v %s\n", msg.Context, ts, string(msg.Data))
	}
}

// newLogMessage is just a helper to build a new log message.
func newLogMessage(msgctx api.LogContext, format string, vs ...interface{}) api.LogMessage {
	return api.LogMessage{
		Context:   msgctx,
		Timestamp: ptypes.MustTimestampProto(time.Now()),
		Data:      []byte(fmt.Sprintf(format, vs...)),
	}
}
