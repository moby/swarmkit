package jobs

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"context"
	"net"
	"os"
	"time"

	"google.golang.org/grpc"

	"github.com/moby/swarmkit/v2/api"
	"github.com/moby/swarmkit/v2/manager/controlapi"
	"github.com/moby/swarmkit/v2/manager/orchestrator/testutils"
	"github.com/moby/swarmkit/v2/manager/state/store"
	stateutils "github.com/moby/swarmkit/v2/manager/state/testutils"
)

var _ = Describe("Integration between the controlapi and jobs orchestrator", func() {
	// These tests verify that the control api and the jobs orchestrator
	// cooperate and work correctly.

	var (
		// the object store is shared between the controlapi server and the
		// orchestrator. makes an end-run around the manager, allowing us to
		// avoid having to start the whole manager apparatus to test this one
		// integration point
		s *store.MemoryStore
		o *Orchestrator

		client api.ControlClient

		// we need all of these variables captured so that we can close them
		// out in AfterEach
		server         *controlapi.Server
		grpcServer     *grpc.Server
		conn           *grpc.ClientConn
		tempUnixSocket string

		// serverDone is a channel that is closed when the goroutine running
		// the server exits.
		serverDone <-chan struct{}
		// orchestratorDone is likewise closed when the orchestrator exits
		orchestratorDone <-chan struct{}
	)

	BeforeEach(func() {
		// By statements are just extra documentation of what's going on, no
		// functional difference besides some test log output
		By("setting up the controlapi client and server")
		// a lot of this setup is taken from manager/controlapi/server_test.go,
		// but it's unexported. in any case, re-writing it in ginkgo isn't
		// unwarranted.
		s = store.NewMemoryStore(&stateutils.MockProposer{})
		server = controlapi.NewServer(s, nil, nil, nil, nil, nil)

		// we need a temporary unix socket to server on
		temp, err := os.CreateTemp("", "test-socket")
		// this is probably to make sure that the socket can be created
		// successfully.
		Expect(err).ToNot(HaveOccurred())
		Expect(temp.Close()).ToNot(HaveOccurred())
		Expect(os.Remove(temp.Name())).ToNot(HaveOccurred())

		tempUnixSocket = temp.Name()
		lis, err := net.Listen("unix", tempUnixSocket)
		Expect(err).ToNot(HaveOccurred())

		grpcServer = grpc.NewServer()
		api.RegisterControlServer(grpcServer, server)

		serverDone = testutils.EnsureRuns(func() {
			_ = grpcServer.Serve(lis)
		})

		// the controlapi tests use the older grpc.Dial with a manually entered
		// timeout. avoid that mess by just using the DialTimeout function
		// instead.
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		// cancel after dial has completed is a no-op, but if we don't cancel,
		// linters will (probably) complain about a leaked context.
		defer cancel()
		conn, err = grpc.DialContext(
			ctx, "unix:"+tempUnixSocket,
			// block on making this connection, to avoid the tests failing for
			// funny reasons related to this connection being established async
			grpc.WithBlock(),
			grpc.WithInsecure(),
		)
		Expect(err).ToNot(HaveOccurred())

		client = api.NewControlClient(conn)

		By("setting up the orchestrator")
		o = NewOrchestrator(s)
		orchestratorDone = testutils.EnsureRuns(func() {
			o.Run(context.Background())
		})
	})

	AfterEach(func() {
		conn.Close()
		grpcServer.Stop()
		// wait for the server to stop
		<-serverDone
		s.Close()
		os.RemoveAll(tempUnixSocket)

		o.Stop()
		<-orchestratorDone
	})

	It("should create the requisite tasks for a new replicated job", func() {
		spec := &api.ServiceSpec{
			Annotations: api.Annotations{
				Name: "testService",
			},
			Mode: &api.ServiceSpec_ReplicatedJob{
				ReplicatedJob: &api.ReplicatedJob{
					MaxConcurrent:    3,
					TotalCompletions: 5,
				},
			},
			Task: api.TaskSpec{
				Runtime: &api.TaskSpec_Container{
					Container: &api.ContainerSpec{
						Image: "image",
					},
				},
			},
		}

		resp, err := client.CreateService(context.Background(), &api.CreateServiceRequest{
			Spec: spec,
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(resp).ToNot(BeNil())
		Expect(resp.Service).ToNot(BeNil())

		// Eventually is like Expect but it does polling, which is what we want
		// If the function has two return values, this will additionally assert
		// that the second value is nil
		Eventually(func() ([]*api.Task, error) {
			// make sure that we can read the tasks back out through the
			// controlapi, not just through the store
			taskListResp, err := client.ListTasks(
				context.Background(),
				&api.ListTasksRequest{
					Filters: &api.ListTasksRequest_Filters{
						ServiceIDs: []string{resp.Service.ID},
						DesiredStates: []api.TaskState{
							api.TaskStateCompleted,
						},
					},
				},
			)
			return taskListResp.Tasks, err
		}).Should(HaveLen(3))
	})
})
