package scheduler

import (
	"fmt"
	"strings"
	"time"

	"github.com/docker/swarmkit/api"
	"github.com/docker/swarmkit/log"
	"golang.org/x/net/context"
)

type tprTaskStore map[string]map[string]*api.Set

// NodeInfo contains a node and some additional metadata.
type NodeInfo struct {
	*api.Node
	Tasks                     map[string]*api.Task
	ActiveTasksCount          int
	ActiveTasksCountByService map[string]int
	AvailableResources        api.Resources

	// Maps tasks to resource taken usage: `map[taskID][resourceName]`
	// This is needed to keep track of the resources where the user specified
	// an int for a node which advertises a set for the specified resource
	ThirdPartyResourcesTaken tprTaskStore

	// recentFailures is a map from service ID to the timestamps of the
	// most recent failures the node has experienced from replicas of that
	// service.
	// TODO(aaronl): When spec versioning is supported, this should track
	// the version of the spec that failed.
	recentFailures map[string][]time.Time
}

func newNodeInfo(n *api.Node, tasks map[string]*api.Task, availableResources api.Resources) NodeInfo {
	nodeInfo := NodeInfo{
		Node:  n,
		Tasks: make(map[string]*api.Task),
		ActiveTasksCountByService: make(map[string]int),
		AvailableResources:        availableResources,
		ThirdPartyResourcesTaken:  make(tprTaskStore),
		recentFailures:            make(map[string][]time.Time),
	}

	for _, t := range tasks {
		nodeInfo.addTask(t)
	}

	return nodeInfo
}

func reclaimTPR(nodeRes *api.ThirdPartyResource, taskRes *api.ThirdPartyResource, taken *api.Set) {
	t := taskRes.Resource.(*api.ThirdPartyResource_Integer).Integer.Val

	switch nr := nodeRes.Resource.(type) {
	case *api.ThirdPartyResource_Integer:
		nr.Integer.Val += t
	case *api.ThirdPartyResource_Set:
		voidVal := &api.Set_Void{}

		for k := range taken.Val {
			nr.Set.Val[k] = voidVal
		}
	}
}

// ClaimThirdPartyResource assign TPRs to the task by taking them from the node's TPR
// and storing them in the taken store
func ClaimThirdPartyResource(nodeRes *api.ThirdPartyResource, taskRes *api.ThirdPartyResource, taken *api.Set) {
	t := taskRes.Resource.(*api.ThirdPartyResource_Integer).Integer.Val

	switch nr := nodeRes.Resource.(type) {
	case *api.ThirdPartyResource_Integer:
		nr.Integer.Val -= t
	case *api.ThirdPartyResource_Set:
		voidVal := &api.Set_Void{}
		i := uint64(0)

		for k := range nr.Set.Val {
			if i >= t {
				return
			}

			i++

			taken.Val[k] = voidVal
			delete(nr.Set.Val, k)
		}
	}
}

// removeTask removes a task from nodeInfo if it's tracked there, and returns true
// if nodeInfo was modified.
func (nodeInfo *NodeInfo) removeTask(t *api.Task) bool {
	oldTask, ok := nodeInfo.Tasks[t.ID]
	if !ok {
		return false
	}

	delete(nodeInfo.Tasks, t.ID)
	if oldTask.DesiredState <= api.TaskStateRunning {
		nodeInfo.ActiveTasksCount--
		nodeInfo.ActiveTasksCountByService[t.ServiceID]--
	}

	reservations := taskReservations(t.Spec)
	resources := &nodeInfo.AvailableResources

	resources.MemoryBytes += reservations.MemoryBytes
	resources.NanoCPUs += reservations.NanoCPUs

	taken := nodeInfo.ThirdPartyResourcesTaken

	for k, v := range reservations.ThirdParty {
		reclaimTPR(resources.ThirdParty[k], v, taken[t.ID][k])
	}

	delete(taken, t.ID)

	return true
}

func setEnvFromTPR(t *api.Task, k string, nodeRes *api.ThirdPartyResource, taskRes *api.ThirdPartyResource, taken *api.Set) {
	tr := taskRes.Resource.(*api.ThirdPartyResource_Integer).Integer.Val

	switch nodeRes.Resource.(type) {
	case *api.ThirdPartyResource_Integer:
		setEnv(t, fmt.Sprintf("%s=%d", k, tr))
	case *api.ThirdPartyResource_Set:
		set := make([]string, len(taken.Val))

		i := 0
		for k := range taken.Val {
			set[i] = k
			i++
		}

		setEnv(t, fmt.Sprintf("%s=%s", k, strings.Join(set, ",")))
	}
}

func setEnv(t *api.Task, env string) {
	switch r := t.Spec.GetRuntime().(type) {
	case *api.TaskSpec_Container:
		r.Container.Env = append(r.Container.Env, env)
	default:
	}
}

// addTask adds or updates a task on nodeInfo, and returns true if nodeInfo was
// modified.
func (nodeInfo *NodeInfo) addTask(t *api.Task) bool {
	oldTask, ok := nodeInfo.Tasks[t.ID]
	if ok {
		if t.DesiredState <= api.TaskStateRunning && oldTask.DesiredState > api.TaskStateRunning {
			nodeInfo.Tasks[t.ID] = t
			nodeInfo.ActiveTasksCount++
			nodeInfo.ActiveTasksCountByService[t.ServiceID]++
			return true
		} else if t.DesiredState > api.TaskStateRunning && oldTask.DesiredState <= api.TaskStateRunning {
			nodeInfo.Tasks[t.ID] = t
			nodeInfo.ActiveTasksCount--
			nodeInfo.ActiveTasksCountByService[t.ServiceID]--
			return true
		}
		return false
	}

	nodeInfo.Tasks[t.ID] = t

	reservations := taskReservations(t.Spec)
	resources := &nodeInfo.AvailableResources

	resources.MemoryBytes -= reservations.MemoryBytes
	resources.NanoCPUs -= reservations.NanoCPUs

	taken := nodeInfo.ThirdPartyResourcesTaken
	taken[t.ID] = make(map[string]*api.Set)

	for k, v := range reservations.ThirdParty {
		taken[t.ID][k] = &api.Set{Val: make(map[string]*api.Set_Void)}
		nodeRes := resources.ThirdParty[k]
		tk := taken[t.ID][k]

		ClaimThirdPartyResource(nodeRes, v, tk)
		setEnvFromTPR(t, k, nodeRes, v, tk)
	}

	if t.DesiredState <= api.TaskStateRunning {
		nodeInfo.ActiveTasksCount++
		nodeInfo.ActiveTasksCountByService[t.ServiceID]++
	}

	return true
}

func taskReservations(spec api.TaskSpec) (reservations api.Resources) {
	if spec.Resources != nil && spec.Resources.Reservations != nil {
		reservations = *spec.Resources.Reservations
	}
	return
}

// taskFailed records a task failure from a given service.
func (nodeInfo *NodeInfo) taskFailed(ctx context.Context, serviceID string) {
	expired := 0
	now := time.Now()
	for _, timestamp := range nodeInfo.recentFailures[serviceID] {
		if now.Sub(timestamp) < monitorFailures {
			break
		}
		expired++
	}

	if len(nodeInfo.recentFailures[serviceID])-expired == maxFailures-1 {
		log.G(ctx).Warnf("underweighting node %s for service %s because it experienced %d failures or rejections within %s", nodeInfo.ID, serviceID, maxFailures, monitorFailures.String())
	}

	nodeInfo.recentFailures[serviceID] = append(nodeInfo.recentFailures[serviceID][expired:], now)
}

// countRecentFailures returns the number of times the service has failed on
// this node within the lookback window monitorFailures.
func (nodeInfo *NodeInfo) countRecentFailures(now time.Time, serviceID string) int {
	recentFailureCount := len(nodeInfo.recentFailures[serviceID])
	for i := recentFailureCount - 1; i >= 0; i-- {
		if now.Sub(nodeInfo.recentFailures[serviceID][i]) > monitorFailures {
			recentFailureCount -= i + 1
			break
		}
	}

	return recentFailureCount
}
