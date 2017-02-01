package state

import (
	"strings"

	"github.com/docker/go-events"
	"github.com/docker/swarmkit/api"
	"github.com/docker/swarmkit/watch"
)

// EventCommit delineates a transaction boundary.
type EventCommit struct {
	Version *api.Version
}

// Matches returns true if this event is a commit event.
func (e EventCommit) Matches(watchEvent events.Event) bool {
	_, ok := watchEvent.(EventCommit)
	return ok
}

func checkCustom(a1, a2 api.Annotations) bool {
	if len(a1.Indices) == 1 {
		for _, ind := range a2.Indices {
			if ind.Key == a1.Indices[0].Key && ind.Val == a1.Indices[0].Val {
				return true
			}
		}
	}
	return false
}

func checkCustomPrefix(a1, a2 api.Annotations) bool {
	if len(a1.Indices) == 1 {
		for _, ind := range a2.Indices {
			if ind.Key == a1.Indices[0].Key && strings.HasPrefix(ind.Val, a1.Indices[0].Val) {
				return true
			}
		}
	}
	return false
}

// TaskCheckID is a TaskCheckFunc for matching task IDs.
func TaskCheckID(t1, t2 *api.Task) bool {
	return t1.ID == t2.ID
}

// TaskCheckIDPrefix is a TaskCheckFunc for matching task IDs by prefix.
func TaskCheckIDPrefix(t1, t2 *api.Task) bool {
	return strings.HasPrefix(t2.ID, t1.ID)
}

// TaskCheckCustom is a TaskCheckFunc for matching task custom indices.
func TaskCheckCustom(t1, t2 *api.Task) bool {
	return checkCustom(t1.Annotations, t2.Annotations)
}

// TaskCheckCustomPrefix is a TaskCheckFunc for matching task custom indices by prefix.
func TaskCheckCustomPrefix(t1, t2 *api.Task) bool {
	return checkCustomPrefix(t1.Annotations, t2.Annotations)
}

// TaskCheckNodeID is a TaskCheckFunc for matching node IDs.
func TaskCheckNodeID(t1, t2 *api.Task) bool {
	return t1.NodeID == t2.NodeID
}

// TaskCheckServiceID is a TaskCheckFunc for matching service IDs.
func TaskCheckServiceID(t1, t2 *api.Task) bool {
	return t1.ServiceID == t2.ServiceID
}

// TaskCheckSlot is a TaskCheckFunc for matching slots.
func TaskCheckSlot(t1, t2 *api.Task) bool {
	return t1.Slot == t2.Slot
}

// TaskCheckDesiredState is a TaskCheckFunc for matching desired state.
func TaskCheckDesiredState(t1, t2 *api.Task) bool {
	return t1.DesiredState == t2.DesiredState
}

// TaskCheckStateGreaterThan is a TaskCheckFunc for checking task state.
func TaskCheckStateGreaterThan(t1, t2 *api.Task) bool {
	return t2.Status.State > t1.Status.State
}

// ServiceCheckID is a ServiceCheckFunc for matching service IDs.
func ServiceCheckID(s1, s2 *api.Service) bool {
	return s1.ID == s2.ID
}

// ServiceCheckIDPrefix is a ServiceCheckFunc for matching service IDs by prefix.
func ServiceCheckIDPrefix(s1, s2 *api.Service) bool {
	return strings.HasPrefix(s2.ID, s1.ID)
}

// ServiceCheckName is a ServiceCheckFunc for matching service names.
func ServiceCheckName(s1, s2 *api.Service) bool {
	return s1.Spec.Annotations.Name == s2.Spec.Annotations.Name
}

// ServiceCheckNamePrefix is a ServiceCheckFunc for matching service names by prefix.
func ServiceCheckNamePrefix(s1, s2 *api.Service) bool {
	return strings.HasPrefix(s2.Spec.Annotations.Name, s1.Spec.Annotations.Name)
}

// ServiceCheckCustom is a ServiceCheckFunc for matching service custom indices.
func ServiceCheckCustom(s1, s2 *api.Service) bool {
	return checkCustom(s1.Spec.Annotations, s2.Spec.Annotations)
}

// ServiceCheckCustomPrefix is a ServiceCheckFunc for matching service custom indices by prefix.
func ServiceCheckCustomPrefix(s1, s2 *api.Service) bool {
	return checkCustomPrefix(s1.Spec.Annotations, s2.Spec.Annotations)
}

// NetworkCheckID is a NetworkCheckFunc for matching network IDs.
func NetworkCheckID(n1, n2 *api.Network) bool {
	return n1.ID == n2.ID
}

// NetworkCheckIDPrefix is a NetworkCheckFunc for matching network IDs by prefix.
func NetworkCheckIDPrefix(n1, n2 *api.Network) bool {
	return strings.HasPrefix(n2.ID, n1.ID)
}

// NetworkCheckName is a NetworkCheckFunc for matching network names.
func NetworkCheckName(n1, n2 *api.Network) bool {
	return n1.Spec.Annotations.Name == n2.Spec.Annotations.Name
}

// NetworkCheckNamePrefix is a NetworkCheckFunc for matching network names by prefix.
func NetworkCheckNamePrefix(n1, n2 *api.Network) bool {
	return strings.HasPrefix(n2.Spec.Annotations.Name, n1.Spec.Annotations.Name)
}

// NetworkCheckCustom is a NetworkCheckFunc for matching network custom indices.
func NetworkCheckCustom(n1, n2 *api.Network) bool {
	return checkCustom(n1.Spec.Annotations, n2.Spec.Annotations)
}

// NetworkCheckCustomPrefix is a NetworkCheckFunc for matching network custom indices by prefix.
func NetworkCheckCustomPrefix(n1, n2 *api.Network) bool {
	return checkCustomPrefix(n1.Spec.Annotations, n2.Spec.Annotations)
}

// NodeCheckID is a NodeCheckFunc for matching node IDs.
func NodeCheckID(n1, n2 *api.Node) bool {
	return n1.ID == n2.ID
}

// NodeCheckIDPrefix is a NodeCheckFunc for matching node IDs by prefix.
func NodeCheckIDPrefix(n1, n2 *api.Node) bool {
	return strings.HasPrefix(n2.ID, n1.ID)
}

// NodeCheckName is a NodeCheckFunc for matching node names.
func NodeCheckName(n1, n2 *api.Node) bool {
	if n1.Description == nil || n2.Description == nil {
		return false
	}
	return n1.Description.Hostname == n2.Description.Hostname
}

// NodeCheckNamePrefix is a NodeCheckFunc for matching node names by prefix.
func NodeCheckNamePrefix(n1, n2 *api.Node) bool {
	if n1.Description == nil || n2.Description == nil {
		return false
	}
	return strings.HasPrefix(n2.Description.Hostname, n1.Description.Hostname)
}

// NodeCheckCustom is a NodeCheckFunc for matching node custom indices.
func NodeCheckCustom(n1, n2 *api.Node) bool {
	return checkCustom(n1.Spec.Annotations, n2.Spec.Annotations)
}

// NodeCheckCustomPrefix is a NodeCheckFunc for matching node custom indices by prefix.
func NodeCheckCustomPrefix(n1, n2 *api.Node) bool {
	return checkCustomPrefix(n1.Spec.Annotations, n2.Spec.Annotations)
}

// NodeCheckState is a NodeCheckFunc for matching node state.
func NodeCheckState(n1, n2 *api.Node) bool {
	return n1.Status.State == n2.Status.State
}

// NodeCheckRole is a NodeCheckFunc for matching node role.
func NodeCheckRole(n1, n2 *api.Node) bool {
	return n1.Role == n2.Role
}

// NodeCheckMembership is a NodeCheckFunc for matching node membership.
func NodeCheckMembership(n1, n2 *api.Node) bool {
	return n1.Spec.Membership == n2.Spec.Membership
}

// ClusterCheckID is a ClusterCheckFunc for matching volume IDs.
func ClusterCheckID(c1, c2 *api.Cluster) bool {
	return c1.ID == c2.ID
}

// ClusterCheckIDPrefix is a ClusterCheckFunc for matching cluster IDs by prefix.
func ClusterCheckIDPrefix(c1, c2 *api.Cluster) bool {
	return strings.HasPrefix(c2.ID, c1.ID)
}

// ClusterCheckName is a ClusterCheckFunc for matching cluster names.
func ClusterCheckName(c1, c2 *api.Cluster) bool {
	return c1.Spec.Annotations.Name == c2.Spec.Annotations.Name
}

// ClusterCheckNamePrefix is a ClusterCheckFunc for matching cluster names by prefix.
func ClusterCheckNamePrefix(c1, c2 *api.Cluster) bool {
	return strings.HasPrefix(c2.Spec.Annotations.Name, c1.Spec.Annotations.Name)
}

// ClusterCheckCustom is a ClusterCheckFunc for matching cluster custom indices.
func ClusterCheckCustom(c1, c2 *api.Cluster) bool {
	return checkCustom(c1.Spec.Annotations, c2.Spec.Annotations)
}

// ClusterCheckCustomPrefix is a ClusterCheckFunc for matching cluster custom indices by prefix.
func ClusterCheckCustomPrefix(c1, c2 *api.Cluster) bool {
	return checkCustomPrefix(c1.Spec.Annotations, c2.Spec.Annotations)
}

// SecretCheckID is a SecretCheckFunc for matching secret IDs.
func SecretCheckID(s1, s2 *api.Secret) bool {
	return s1.ID == s2.ID
}

// SecretCheckIDPrefix is a SecretCheckFunc for matching secret IDs by prefix.
func SecretCheckIDPrefix(s1, s2 *api.Secret) bool {
	return strings.HasPrefix(s2.ID, s1.ID)
}

// SecretCheckName is a SecretCheckFunc for matching secret names.
func SecretCheckName(s1, s2 *api.Secret) bool {
	return s1.Spec.Annotations.Name == s2.Spec.Annotations.Name
}

// SecretCheckNamePrefix is a SecretCheckFunc for matching secret names by prefix.
func SecretCheckNamePrefix(s1, s2 *api.Secret) bool {
	return strings.HasPrefix(s2.Spec.Annotations.Name, s1.Spec.Annotations.Name)
}

// SecretCheckCustom is a SecretCheckFunc for matching secret custom indices.
func SecretCheckCustom(s1, s2 *api.Secret) bool {
	return checkCustom(s1.Spec.Annotations, s2.Spec.Annotations)
}

// SecretCheckCustomPrefix is a SecretCheckFunc for matching secret custom indices by prefix.
func SecretCheckCustomPrefix(s1, s2 *api.Secret) bool {
	return checkCustomPrefix(s1.Spec.Annotations, s2.Spec.Annotations)
}

// ResourceCheckID is a ResourceCheckFunc for matching resource IDs.
func ResourceCheckID(v1, v2 *api.Resource) bool {
	return v1.ID == v2.ID
}

// ResourceCheckKind is a ResourceCheckFunc for matching resource kinds.
func ResourceCheckKind(v1, v2 *api.Resource) bool {
	return v1.Kind == v2.Kind
}

// ResourceCheckIDPrefix is a ResourceCheckFunc for matching resource IDs by prefix.
func ResourceCheckIDPrefix(v1, v2 *api.Resource) bool {
	return strings.HasPrefix(v2.ID, v1.ID)
}

// ResourceCheckName is a ResourceCheckFunc for matching resource names.
func ResourceCheckName(v1, v2 *api.Resource) bool {
	return v1.Annotations.Name == v2.Annotations.Name
}

// ResourceCheckNamePrefix is a ResourceCheckFunc for matching resource names by prefix.
func ResourceCheckNamePrefix(v1, v2 *api.Resource) bool {
	return strings.HasPrefix(v2.Annotations.Name, v1.Annotations.Name)
}

// ResourceCheckCustom is a ResourceCheckFunc for matching resource custom indices.
func ResourceCheckCustom(v1, v2 *api.Resource) bool {
	return checkCustom(v1.Annotations, v2.Annotations)
}

// ResourceCheckCustomPrefix is a ResourceCheckFunc for matching resource custom indices by prefix.
func ResourceCheckCustomPrefix(v1, v2 *api.Resource) bool {
	return checkCustomPrefix(v1.Annotations, v2.Annotations)
}

// ExtensionCheckID is a ExtensionCheckFunc for matching extension IDs.
func ExtensionCheckID(v1, v2 *api.Extension) bool {
	return v1.ID == v2.ID
}

// ExtensionCheckIDPrefix is a ExtensionCheckFunc for matching extension IDs by
// prefix.
func ExtensionCheckIDPrefix(s1, s2 *api.Extension) bool {
	return strings.HasPrefix(s2.ID, s1.ID)
}

// ExtensionCheckName is a ExtensionCheckFunc for matching extension names.
func ExtensionCheckName(v1, v2 *api.Extension) bool {
	return v1.Annotations.Name == v2.Annotations.Name
}

// ExtensionCheckNamePrefix is a ExtensionCheckFunc for matching extension
// names by prefix.
func ExtensionCheckNamePrefix(v1, v2 *api.Extension) bool {
	return strings.HasPrefix(v2.Annotations.Name, v1.Annotations.Name)
}

// ExtensionCheckCustom is a ExtensionCheckFunc for matching extension custom
// indices.
func ExtensionCheckCustom(v1, v2 *api.Extension) bool {
	return checkCustom(v1.Annotations, v2.Annotations)
}

// ExtensionCheckCustomPrefix is a ExtensionCheckFunc for matching extension
// custom indices by prefix.
func ExtensionCheckCustomPrefix(v1, v2 *api.Extension) bool {
	return checkCustomPrefix(v1.Annotations, v2.Annotations)
}

// Watch takes a variable number of events to match against. The subscriber
// will receive events that match any of the arguments passed to Watch.
//
// Examples:
//
// // subscribe to all events
// Watch(q)
//
// // subscribe to all UpdateTask events
// Watch(q, EventUpdateTask{})
//
// // subscribe to all task-related events
// Watch(q, EventUpdateTask{}, EventCreateTask{}, EventDeleteTask{})
//
// // subscribe to UpdateTask for node 123
// Watch(q, EventUpdateTask{Task: &api.Task{NodeID: 123},
//                         Checks: []TaskCheckFunc{TaskCheckNodeID}})
//
// // subscribe to UpdateTask for node 123, as well as CreateTask
// // for node 123 that also has ServiceID set to "abc"
// Watch(q, EventUpdateTask{Task: &api.Task{NodeID: 123},
//                         Checks: []TaskCheckFunc{TaskCheckNodeID}},
//         EventCreateTask{Task: &api.Task{NodeID: 123, ServiceID: "abc"},
//                         Checks: []TaskCheckFunc{TaskCheckNodeID,
//                                                 func(t1, t2 *api.Task) bool {
//                                                         return t1.ServiceID == t2.ServiceID
//                                                 }}})
func Watch(queue *watch.Queue, specifiers ...api.Event) (eventq chan events.Event, cancel func()) {
	if len(specifiers) == 0 {
		return queue.Watch()
	}
	return queue.CallbackWatch(Matcher(specifiers...))
}

// Matcher returns an events.Matcher that Matches the specifiers with OR logic.
func Matcher(specifiers ...api.Event) events.MatcherFunc {
	return events.MatcherFunc(func(event events.Event) bool {
		for _, s := range specifiers {
			if s.Matches(event) {
				return true
			}
		}
		return false
	})
}
