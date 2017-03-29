package integration

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"runtime"

	"google.golang.org/grpc"

	"github.com/docker/swarmkit/api"
	"github.com/docker/swarmkit/ca"
	"github.com/docker/swarmkit/identity"
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
// It uses TestExecutor as executor. If lateBind is set, the remote API port is not
// bound.  If rootCA is set, this root is used to bootstrap the node's TLS certs.
func newTestNode(joinAddr, joinToken string, lateBind bool, rootCA *ca.RootCA) (*testNode, error) {
	tmpDir, err := ioutil.TempDir("", "swarmkit-integration-")
	if err != nil {
		return nil, err
	}

	cAddr := filepath.Join(tmpDir, "control.sock")
	cfg := &node.Config{
		ListenControlAPI: cAddr,
		JoinAddr:         joinAddr,
		StateDir:         tmpDir,
		Executor:         &TestExecutor{},
		JoinToken:        joinToken,
	}
	if !lateBind {
		cfg.ListenRemoteAPI = "127.0.0.1:0"
	}
	if rootCA != nil {
		signer, err := rootCA.Signer()
		if err != nil {
			return nil, err
		}
		certDir := filepath.Join(tmpDir, "certificates")
		if err := os.MkdirAll(certDir, 0700); err != nil {
			return nil, err
		}
		certPaths := ca.NewConfigPaths(certDir)
		if err := ioutil.WriteFile(certPaths.RootCA.Cert, signer.Cert, 0644); err != nil {
			return nil, err
		}
		if err := ioutil.WriteFile(certPaths.RootCA.Key, signer.Key, 0600); err != nil {
			return nil, err
		}
		// generate TLS certs for this manager for bootstrapping, else the node will generate its own CA
		_, _, err = rootCA.IssueAndSaveNewCertificates(
			ca.NewKeyReadWriter(certPaths.Node, nil, nil),
			identity.NewID(), ca.ManagerRole, identity.NewID())
		if err != nil {
			return nil, err
		}
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

// Pause stops the node, and creates a new swarm node while keeping all the state
func (n *testNode) Pause(forceNewCluster bool) error {
	rAddr, err := n.node.RemoteAPIAddr()
	if err != nil {
		rAddr = "127.0.0.1:0"
	}

	if err := n.stop(); err != nil {
		return err
	}

	cfg := n.config
	cfg.ListenRemoteAPI = rAddr
	// If JoinAddr is set, the node will connect to the join addr and ignore any
	// other remotes that are stored in the raft directory.
	cfg.JoinAddr = ""
	cfg.JoinToken = ""
	cfg.ForceNewCluster = forceNewCluster

	node, err := node.New(cfg)
	if err != nil {
		return err
	}
	n.node = node
	return nil
}

func (n *testNode) stop() error {
	ctx, cancel := context.WithTimeout(context.Background(), opsTimeout)
	defer cancel()
	isManager := n.IsManager()
	if err := n.node.Stop(ctx); err != nil {
		// TODO(aaronl): This stack dumping may be removed in the
		// future once context deadline issues while shutting down
		// nodes are resolved.
		buf := make([]byte, 1024)
		for {
			n := runtime.Stack(buf, true)
			if n < len(buf) {
				buf = buf[:n]
				break
			}
			buf = make([]byte, 2*len(buf))
		}
		os.Stderr.Write(buf)

		if isManager {
			return fmt.Errorf("error stop manager %s: %v", n.node.NodeID(), err)
		}
		return fmt.Errorf("error stop worker %s: %v", n.node.NodeID(), err)
	}
	return nil
}

// Stop stops the node and removes its state directory.
func (n *testNode) Stop() error {
	if err := n.stop(); err != nil {
		return err
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
	return n.node.Manager() != nil
}
