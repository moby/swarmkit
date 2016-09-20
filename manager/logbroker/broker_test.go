package logbroker

import (
	"context"
	"fmt"
	"net"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc"

	"github.com/docker/swarmkit/api"
	"github.com/docker/swarmkit/protobuf/ptypes"
)

func TestLogBroker(t *testing.T) {
	ctx, _, client, brokerClient, done := testLogBrokerEnv(t)
	defer done()

	var (
		wg               sync.WaitGroup
		hold             = make(chan struct{}) // coordinates pubsub start
		messagesExpected int
	)

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
					defer wg.Done()

					var messages []api.LogMessage
					msgctx := api.LogContext{
						NodeID:    nodeID,
						ServiceID: serviceID,
						TaskID:    taskID,
					}
					for i := 0; i < nLogMessagesPerTask; i++ {
						messages = append(messages, newLogMessage(msgctx, "log message number %d", i))
					}

					if _, err := brokerClient.PublishLogs(ctx, &api.PublishLogsRequest{
						Messages: messages,
					}); err != nil {
						t.Fatalf("error publishing log message: %v")
					}
				}(nodeID, serviceID, taskID)
			}
		}
	}

	stream, err := client.SubscribeLogs(ctx, &api.SubscribeLogsRequest{})
	if err != nil {
		t.Fatalf("error subscribing: %v", err)
	}

	t.Logf("expected %v messages", messagesExpected)
	close(hold)
	var messages int
	for messages < messagesExpected {
		msgs, err := stream.Recv()
		if err != nil {
			t.Fatalf("error recv stream: %v", err)
		}

		for range msgs.Messages {
			messages++
			// ts, _ := ptypes.Timestamp(msg.Timestamp)
			// fmt.Printf("%v %v %v %s\n", messages, msgs.Context, ts, string(msg.Data))

			if messages%100 == 0 {
				fmt.Println(messages, "received")
			}
		}
	}
	t.Logf("received %v messages", messages)

	wg.Wait()
}

func testLogBrokerEnv(t *testing.T) (context.Context, *LogBroker, api.LogsClient, api.LogBrokerClient, func()) {
	ctx, cancel := context.WithCancel(context.Background())
	broker := NewLogBroker()
	server := grpc.NewServer()
	api.RegisterLogBrokerServer(server, broker)
	api.RegisterLogsServer(server, broker)

	listener, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("error setting up listener: %v", err)
	}

	go func() {
		if err := server.Serve(listener); err != nil {
			// SIGH(stevvooe): GRPC won't really shutdown gracefully.
			// This should be fatal.
			t.Logf("error serving grpc service: %v", err)
		}
	}()

	cc, err := grpc.Dial(listener.Addr().String(), grpc.WithInsecure())
	if err != nil {
		t.Fatalf("error dialing local server: %v", err)
	}

	brokerClient := api.NewLogBrokerClient(cc)
	client := api.NewLogsClient(cc)

	return ctx, broker, client, brokerClient, func() {
		cc.Close()
		server.Stop()
		listener.Close()
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
