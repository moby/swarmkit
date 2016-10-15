package integration

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"

	"google.golang.org/grpc"

	"github.com/docker/swarmkit/api"
	raftutils "github.com/docker/swarmkit/manager/state/raft/testutils"
	"github.com/docker/swarmkit/node"
	"golang.org/x/net/context"
)

// TestNode is representation of *agent.Node. It stores listeners, connections,
// config for later access from tests.
type testNode struct {
	config   *node.Config
	node     *node.Node
	stateDir string
}

// newNode creates new node with specific role(manager or agent) and joins to
// existing cluster. if joinAddr is empty string, then new cluster will be initialized.
// It uses TestExecutor as executor.
func newTestNode(joinAddr, joinToken string) (*testNode, error) {
	tmpDir, err := ioutil.TempDir("", "swarmkit-integration-")
	if err != nil {
		return nil, err
	}

	rAddr := "127.0.0.1:0"
	cAddr := filepath.Join(tmpDir, "control.sock")
	cfg := &node.Config{
		ListenRemoteAPI:  rAddr,
		ListenControlAPI: cAddr,
		JoinAddr:         joinAddr,
		StateDir:         tmpDir,
		Executor:         &TestExecutor{},
		JoinToken:        joinToken,
	}
	node, err := node.New(cfg)
	if err != nil {
		return nil, err
	}
	return &testNode{
		config:   cfg,
		node:     node,
		stateDir: tmpDir,
	}, nil
}

// Stop stops the node and removes its state directory.
func (n *testNode) Stop() error {
	ctx, cancel := context.WithTimeout(context.Background(), opsTimeout)
	defer cancel()
	isManager := n.IsManager()
	if err := n.node.Stop(ctx); err != nil {
		if isManager {
			return fmt.Errorf("error stop manager %s: %v", n.node.NodeID(), err)
		}
		return fmt.Errorf("error stop worker %s: %v", n.node.NodeID(), err)
	}
	return os.RemoveAll(n.stateDir)
}

// ControlClient returns grpc client to ControlAPI of node. It will panic for
// non-manager nodes.
func (n *testNode) ControlClient(ctx context.Context) (api.ControlClient, error) {
	ctx, cancel := context.WithTimeout(ctx, opsTimeout)
	defer cancel()
	connChan := n.node.ListenControlSocket(ctx)
	var controlConn *grpc.ClientConn
	if err := raftutils.PollFuncWithTimeout(nil, func() error {
		select {
		case controlConn = <-connChan:
		default:
		}
		if controlConn == nil {
			return fmt.Errorf("didn't get control api connection")
		}
		return nil
	}, opsTimeout); err != nil {
		return nil, err
	}
	return api.NewControlClient(controlConn), nil
}

func (n *testNode) IsManager() bool {
	_, err := n.node.RemoteAPIAddr()
	return err == nil
}
