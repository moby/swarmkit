package containerd

import (
	"net"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/docker/docker/pkg/sysinfo"
	"github.com/docker/docker/pkg/system"
	"github.com/docker/swarmkit/agent/exec"
	"github.com/docker/swarmkit/agent/secrets"
	"github.com/docker/swarmkit/api"
	"github.com/docker/swarmkit/log"
	"github.com/pkg/errors"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/grpclog"
)

type executor struct {
	conn             *grpc.ClientConn
	secrets          exec.SecretsManager
	genericResources []*api.GenericResource
	containerDir     string
}

var _ exec.Executor = &executor{}

func getGRPCConnection(ctx context.Context, sock string) (*grpc.ClientConn, error) {
	grpclog.SetLogger(log.G(ctx))

	dialOpts := []grpc.DialOption{grpc.WithInsecure(), grpc.WithTimeout(100 * time.Second)}
	dialOpts = append(dialOpts,
		grpc.WithDialer(func(addr string, timeout time.Duration) (net.Conn, error) {
			return net.DialTimeout("unix", sock, timeout)
		},
		))

	conn, err := grpc.Dial("unix://"+sock, dialOpts...)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to dial %q", sock)
	}

	return conn, nil
}

// NewExecutor returns an executor using the given containerd control socket
func NewExecutor(sock, stateDir string, genericResources []*api.GenericResource) (exec.Executor, error) {
	ctx := log.WithModule(context.Background(), "containerd")

	conn, err := getGRPCConnection(ctx, sock)
	if err != nil {
		return nil, err
	}

	containerDir := filepath.Join(stateDir, "containers")

	return &executor{
		conn:             conn,
		secrets:          secrets.NewManager(),
		genericResources: genericResources,
		containerDir:     containerDir,
	}, nil
}

// Describe returns the underlying node description from containerd
func (e *executor) Describe(ctx context.Context) (*api.NodeDescription, error) {
	ctx = log.WithModule(ctx, "containerd")

	hostname := ""
	if hn, err := os.Hostname(); err != nil {
		log.G(ctx).Warnf("Could not get hostname: %v", err)
	} else {
		hostname = hn
	}

	meminfo, err := system.ReadMemInfo()
	if err != nil {
		log.G(ctx).WithError(err).Error("Failed to read meminfo")
		meminfo = &system.MemInfo{}
	}

	description := &api.NodeDescription{
		Hostname: hostname,
		Platform: &api.Platform{
			Architecture: runtime.GOARCH,
			OS:           runtime.GOOS,
		},
		Resources: &api.Resources{
			NanoCPUs:    int64(sysinfo.NumCPU()),
			MemoryBytes: meminfo.MemTotal,
			Generic:     e.genericResources,
		},
	}

	return description, nil
}

func (e *executor) Configure(ctx context.Context, node *api.Node) error {
	return nil
}

// Controller returns a docker container controller.
func (e *executor) Controller(t *api.Task) (exec.Controller, error) {
	ctlr, err := newController(e.conn, e.containerDir, t, secrets.Restrict(e.secrets, t))
	if err != nil {
		return nil, err
	}

	return ctlr, nil
}

func (e *executor) SetNetworkBootstrapKeys([]*api.EncryptionKey) error {
	return nil
}

func (e *executor) Secrets() exec.SecretsManager {
	return e.secrets
}
