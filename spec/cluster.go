package spec

import (
	"fmt"
	"io"

	yaml "github.com/cloudfoundry-incubator/candiedyaml"
	"github.com/docker/libswarm/api"
	"github.com/docker/libswarm/ca"
	"github.com/docker/libswarm/manager/state/raft"
	"github.com/pmezard/go-difflib/difflib"
)

// ClusterConfig is the yaml representation of a cluster spec.
type ClusterConfig struct {
	AcceptancePolicy    AcceptancePolicy    `yaml:"acceptancepolicy,omitempty"`
	OrchestrationConfig OrchestrationConfig `yaml:"orchestration,omitempty"`
	RaftConfig          RaftConfig          `yaml:"raft,omitempty"`

	Name string `yaml:"name"`
}

// AcceptancePolicy is the yaml representation of an acceptance policy.
type AcceptancePolicy struct {
	Policies []*RoleAdmissionPolicy
}

// RoleAdmissionPolicy is the yaml representation of a RoleAdmissionPolicy.
type RoleAdmissionPolicy struct {
	Role       string `yaml:"role"`
	Autoaccept bool   `yaml:"autoaccept"`
	Secret     string `yaml:"secret,omitempty"`
}

// OrchestrationConfig is the yaml representation of the cluster-wide
// orchestration settings.
type OrchestrationConfig struct {
	// TaskHistoryRetentionLimit is the number of historic task entries to
	// retain per service instance or node.
	TaskHistoryRetentionLimit int64 `yaml:"taskhistory"`
}

// RaftConfig is the yaml representation of the raft settings.
type RaftConfig struct {
	// SnapshotInterval is the number of log entries between snapshots.
	SnapshotInterval uint64 `yaml:"snapshotinterval"`
	// KeepOldSnapshots is the number of snapshots to keep beyond the
	// current snapshot.
	KeepOldSnapshots uint64 `yaml:"keepoldsnapshots"`
	// LogEntriesForSlowFollowers is the number of log entries to keep
	// around to sync up slow followers after a snapshot is created.
	LogEntriesForSlowFollowers uint64 `yaml:"logentriesforslowfollowers"`
	// ElectionTick defines the amount of ticks needed without
	// leader to trigger a new election.
	ElectionTick uint32 `yaml:"electiontick"`
	// HeartbeatTick defines the amount of ticks between each
	// heartbeat sent to other members for health-check purposes.
	HeartbeatTick uint32 `yaml:"heartbeattick"`
}

// Reset resets the cluster config to its defaults.
func (c *ClusterConfig) Reset() {
	*c = ClusterConfig{}
}

// Read reads a ClusterConfig from an io.Reader.
func (c *ClusterConfig) Read(r io.Reader) error {
	c.Reset()

	if err := yaml.NewDecoder(r).Decode(c); err != nil {
		return err
	}

	return c.Validate()
}

// Write writes a ClusterConfig to an io.Reader.
func (c *ClusterConfig) Write(w io.Writer) error {
	return yaml.NewEncoder(w).Encode(c)
}

// Validate checks the validity of the strategy.
func (c *ClusterConfig) Validate() error {
	if c == nil {
		return nil
	}
	if c.Name == "" {
		return fmt.Errorf("name is mandatory")
	}
	for _, p := range c.AcceptancePolicy.Policies {
		if p.Role != "agent" && p.Role != "manager" {
			return fmt.Errorf("unrecognized role %s", p.Role)
		}
	}
	return nil
}

// ToProto converts native ClusterConfig into protos.
func (c *ClusterConfig) ToProto() *api.ClusterSpec {
	if c == nil {
		return nil
	}
	p := &api.ClusterSpec{
		Annotations: api.Annotations{
			Name: c.Name,
		},
		AcceptancePolicy: api.AcceptancePolicy{
			Policies: []*api.AcceptancePolicy_RoleAdmissionPolicy{
				{
					Role: api.NodeRoleWorker,
				},
				{
					Role: api.NodeRoleWorker,
				},
			},
		},
		Orchestration: api.OrchestrationConfig{
			TaskHistoryRetentionLimit: c.OrchestrationConfig.TaskHistoryRetentionLimit,
		},
		Raft: api.RaftConfig{
			SnapshotInterval:           c.RaftConfig.SnapshotInterval,
			KeepOldSnapshots:           c.RaftConfig.KeepOldSnapshots,
			LogEntriesForSlowFollowers: c.RaftConfig.LogEntriesForSlowFollowers,
			ElectionTick:               c.RaftConfig.ElectionTick,
			HeartbeatTick:              c.RaftConfig.HeartbeatTick,
		},
	}

	for _, policy := range c.AcceptancePolicy.Policies {
		apiRole, err := ca.FormatRole(policy.Role)
		if err != nil {
			continue
		}
		newPolicy := &api.AcceptancePolicy_RoleAdmissionPolicy{
			Role:       apiRole,
			Autoaccept: policy.Autoaccept,
			Secret:     policy.Secret,
		}
		p.AcceptancePolicy.Policies = append(p.AcceptancePolicy.Policies, newPolicy)
	}

	raftDefaults := raft.DefaultRaftConfig()

	if p.Raft.SnapshotInterval == 0 {
		p.Raft.SnapshotInterval = raftDefaults.SnapshotInterval
	}
	if p.Raft.LogEntriesForSlowFollowers == 0 {
		p.Raft.LogEntriesForSlowFollowers = raftDefaults.LogEntriesForSlowFollowers
	}
	if p.Raft.ElectionTick == 0 {
		p.Raft.ElectionTick = raftDefaults.ElectionTick
	}
	if p.Raft.HeartbeatTick == 0 {
		p.Raft.HeartbeatTick = raftDefaults.HeartbeatTick
	}

	return p
}

// FromProto converts proto ClusterSpec back into native types.
func (c *ClusterConfig) FromProto(p *api.ClusterSpec) {
	if p == nil {
		return
	}

	*c = ClusterConfig{
		Name: p.Annotations.Name,
		OrchestrationConfig: OrchestrationConfig{
			TaskHistoryRetentionLimit: p.Orchestration.TaskHistoryRetentionLimit,
		},
		RaftConfig: RaftConfig{
			SnapshotInterval:           p.Raft.SnapshotInterval,
			KeepOldSnapshots:           p.Raft.KeepOldSnapshots,
			LogEntriesForSlowFollowers: p.Raft.LogEntriesForSlowFollowers,
			ElectionTick:               p.Raft.ElectionTick,
			HeartbeatTick:              p.Raft.HeartbeatTick,
		},
	}

	for _, policy := range p.AcceptancePolicy.Policies {
		role, err := ca.ParseRole(policy.Role)
		if err != nil {
			continue
		}
		newPolicy := &RoleAdmissionPolicy{
			Role:       role,
			Autoaccept: policy.Autoaccept,
			Secret:     policy.Secret,
		}
		c.AcceptancePolicy.Policies = append(c.AcceptancePolicy.Policies, newPolicy)
	}

}

// Diff returns a diff between two ClusterConfigs.
func (c *ClusterConfig) Diff(context int, fromFile, toFile string, other *ClusterConfig) (string, error) {
	// Marshal back and forth to make sure we run with the same defaults.
	from := &ClusterConfig{}
	from.FromProto(other.ToProto())

	to := &ClusterConfig{}
	to.FromProto(c.ToProto())

	fromYml, err := yaml.Marshal(from)
	if err != nil {
		return "", err
	}

	toYml, err := yaml.Marshal(to)
	if err != nil {
		return "", err
	}

	diff := difflib.UnifiedDiff{
		A:        difflib.SplitLines(string(fromYml)),
		FromFile: fromFile,
		B:        difflib.SplitLines(string(toYml)),
		ToFile:   toFile,
		Context:  context,
	}

	return difflib.GetUnifiedDiffString(diff)
}
