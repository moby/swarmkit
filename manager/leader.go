package manager

import (
	"github.com/docker/swarmkit/manager/orchestrator"
	"github.com/docker/swarmkit/manager/state/raft"
	"golang.org/x/net/context"
)

// Leader is the cluster leader for Swarm.
type Leader struct {
	globalOrchestrator     *orchestrator.GlobalOrchestrator
	replicatedOrchestrator *orchestrator.ReplicatedOrchestrator
	taskReaper             *orchestrator.TaskReaper
}

// NewLeader creates a new leader
func NewLeader(raftNode *raft.Node) *Leader {

	leader := &Leader{
		globalOrchestrator:     orchestrator.NewGlobalOrchestrator(raftNode.MemoryStore()),
		replicatedOrchestrator: orchestrator.NewReplicatedOrchestrator(raftNode.MemoryStore()),
		taskReaper:             orchestrator.NewTaskReaper(raftNode.MemoryStore()),
	}

	return leader
}

// Start starts leader
func (l *Leader) Start(ctx context.Context) {
	l.globalOrchestrator.Start(ctx)
	l.replicatedOrchestrator.Start(ctx)
	l.taskReaper.Start(ctx)

	// FIXME(jimmyxian): check error and automatically restart components based on error
}

// Stop stop leader
func (l *Leader) Stop(ctx context.Context) {
	l.globalOrchestrator.Stop(ctx)
	l.replicatedOrchestrator.Stop(ctx)
	l.taskReaper.Stop(ctx)
}
