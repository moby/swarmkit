package state

import (
	"strings"

	"github.com/docker/go-events"
	"github.com/docker/swarmkit/api"
	"github.com/docker/swarmkit/watch"
)

// Event is the type used for events passed over watcher channels, and also
// the type used to specify filtering in calls to Watch.
type Event interface {
	// TODO(stevvooe): Consider whether it makes sense to squish both the
	// matcher type and the primary type into the same type. It might be better
	// to build a matcher from an event prototype.

	// matches checks if this item in a watch queue matches the event
	// description.
	matches(events.Event) bool
}

// EventCommit delineates a transaction boundary.
type EventCommit struct {
	Version *api.Version
}

func (e EventCommit) matches(watchEvent events.Event) bool {
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

// TaskCheckFunc is the type of function used to perform filtering checks on
// api.Task structures.
type TaskCheckFunc func(t1, t2 *api.Task) bool

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

// EventCreateTask is the type used to put CreateTask events on the
// publish/subscribe queue and filter these events in calls to Watch.
type EventCreateTask struct {
	Task *api.Task
	// Checks is a list of functions to call to filter events for a watch
	// stream. They are applied with AND logic. They are only applicable for
	// calls to Watch.
	Checks []TaskCheckFunc
}

func (e EventCreateTask) matches(watchEvent events.Event) bool {
	typedEvent, ok := watchEvent.(EventCreateTask)
	if !ok {
		return false
	}

	for _, check := range e.Checks {
		if !check(e.Task, typedEvent.Task) {
			return false
		}
	}
	return true
}

// EventUpdateTask is the type used to put UpdateTask events on the
// publish/subscribe queue and filter these events in calls to Watch.
type EventUpdateTask struct {
	Task *api.Task
	// Checks is a list of functions to call to filter events for a watch
	// stream. They are applied with AND logic. They are only applicable for
	// calls to Watch.
	Checks []TaskCheckFunc
}

func (e EventUpdateTask) matches(watchEvent events.Event) bool {
	typedEvent, ok := watchEvent.(EventUpdateTask)
	if !ok {
		return false
	}

	for _, check := range e.Checks {
		if !check(e.Task, typedEvent.Task) {
			return false
		}
	}
	return true
}

// EventDeleteTask is the type used to put DeleteTask events on the
// publish/subscribe queue and filter these events in calls to Watch.
type EventDeleteTask struct {
	Task *api.Task
	// Checks is a list of functions to call to filter events for a watch
	// stream. They are applied with AND logic. They are only applicable for
	// calls to Watch.
	Checks []TaskCheckFunc
}

func (e EventDeleteTask) matches(watchEvent events.Event) bool {
	typedEvent, ok := watchEvent.(EventDeleteTask)
	if !ok {
		return false
	}

	for _, check := range e.Checks {
		if !check(e.Task, typedEvent.Task) {
			return false
		}
	}
	return true
}

// ServiceCheckFunc is the type of function used to perform filtering checks on
// api.Service structures.
type ServiceCheckFunc func(s1, s2 *api.Service) bool

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

// EventCreateService is the type used to put CreateService events on the
// publish/subscribe queue and filter these events in calls to Watch.
type EventCreateService struct {
	Service *api.Service
	// Checks is a list of functions to call to filter events for a watch
	// stream. They are applied with AND logic. They are only applicable for
	// calls to Watch.
	Checks []ServiceCheckFunc
}

func (e EventCreateService) matches(watchEvent events.Event) bool {
	typedEvent, ok := watchEvent.(EventCreateService)
	if !ok {
		return false
	}

	for _, check := range e.Checks {
		if !check(e.Service, typedEvent.Service) {
			return false
		}
	}
	return true
}

// EventUpdateService is the type used to put UpdateService events on the
// publish/subscribe queue and filter these events in calls to Watch.
type EventUpdateService struct {
	Service *api.Service
	// Checks is a list of functions to call to filter events for a watch
	// stream. They are applied with AND logic. They are only applicable for
	// calls to Watch.
	Checks []ServiceCheckFunc
}

func (e EventUpdateService) matches(watchEvent events.Event) bool {
	typedEvent, ok := watchEvent.(EventUpdateService)
	if !ok {
		return false
	}

	for _, check := range e.Checks {
		if !check(e.Service, typedEvent.Service) {
			return false
		}
	}
	return true
}

// EventDeleteService is the type used to put DeleteService events on the
// publish/subscribe queue and filter these events in calls to Watch.
type EventDeleteService struct {
	Service *api.Service
	// Checks is a list of functions to call to filter events for a watch
	// stream. They are applied with AND logic. They are only applicable for
	// calls to Watch.
	Checks []ServiceCheckFunc
}

func (e EventDeleteService) matches(watchEvent events.Event) bool {
	typedEvent, ok := watchEvent.(EventDeleteService)
	if !ok {
		return false
	}

	for _, check := range e.Checks {
		if !check(e.Service, typedEvent.Service) {
			return false
		}
	}
	return true
}

// NetworkCheckFunc is the type of function used to perform filtering checks on
// api.Service structures.
type NetworkCheckFunc func(n1, n2 *api.Network) bool

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

// EventCreateNetwork is the type used to put CreateNetwork events on the
// publish/subscribe queue and filter these events in calls to Watch.
type EventCreateNetwork struct {
	Network *api.Network
	// Checks is a list of functions to call to filter events for a watch
	// stream. They are applied with AND logic. They are only applicable for
	// calls to Watch.
	Checks []NetworkCheckFunc
}

func (e EventCreateNetwork) matches(watchEvent events.Event) bool {
	typedEvent, ok := watchEvent.(EventCreateNetwork)
	if !ok {
		return false
	}

	for _, check := range e.Checks {
		if !check(e.Network, typedEvent.Network) {
			return false
		}
	}
	return true
}

// EventUpdateNetwork is the type used to put UpdateNetwork events on the
// publish/subscribe queue and filter these events in calls to Watch.
type EventUpdateNetwork struct {
	Network *api.Network
	// Checks is a list of functions to call to filter events for a watch
	// stream. They are applied with AND logic. They are only applicable for
	// calls to Watch.
	Checks []NetworkCheckFunc
}

func (e EventUpdateNetwork) matches(watchEvent events.Event) bool {
	typedEvent, ok := watchEvent.(EventUpdateNetwork)
	if !ok {
		return false
	}

	for _, check := range e.Checks {
		if !check(e.Network, typedEvent.Network) {
			return false
		}
	}
	return true
}

// EventDeleteNetwork is the type used to put DeleteNetwork events on the
// publish/subscribe queue and filter these events in calls to Watch.
type EventDeleteNetwork struct {
	Network *api.Network
	// Checks is a list of functions to call to filter events for a watch
	// stream. They are applied with AND logic. They are only applicable for
	// calls to Watch.
	Checks []NetworkCheckFunc
}

func (e EventDeleteNetwork) matches(watchEvent events.Event) bool {
	typedEvent, ok := watchEvent.(EventDeleteNetwork)
	if !ok {
		return false
	}

	for _, check := range e.Checks {
		if !check(e.Network, typedEvent.Network) {
			return false
		}
	}
	return true
}

// NodeCheckFunc is the type of function used to perform filtering checks on
// api.Service structures.
type NodeCheckFunc func(n1, n2 *api.Node) bool

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

// EventCreateNode is the type used to put CreateNode events on the
// publish/subscribe queue and filter these events in calls to Watch.
type EventCreateNode struct {
	Node *api.Node
	// Checks is a list of functions to call to filter events for a watch
	// stream. They are applied with AND logic. They are only applicable for
	// calls to Watch.
	Checks []NodeCheckFunc
}

func (e EventCreateNode) matches(watchEvent events.Event) bool {
	typedEvent, ok := watchEvent.(EventCreateNode)
	if !ok {
		return false
	}

	for _, check := range e.Checks {
		if !check(e.Node, typedEvent.Node) {
			return false
		}
	}
	return true
}

// EventUpdateNode is the type used to put DeleteNode events on the
// publish/subscribe queue and filter these events in calls to Watch.
type EventUpdateNode struct {
	Node *api.Node
	// Checks is a list of functions to call to filter events for a watch
	// stream. They are applied with AND logic. They are only applicable for
	// calls to Watch.
	Checks []NodeCheckFunc
}

func (e EventUpdateNode) matches(watchEvent events.Event) bool {
	typedEvent, ok := watchEvent.(EventUpdateNode)
	if !ok {
		return false
	}

	for _, check := range e.Checks {
		if !check(e.Node, typedEvent.Node) {
			return false
		}
	}
	return true
}

// EventDeleteNode is the type used to put DeleteNode events on the
// publish/subscribe queue and filter these events in calls to Watch.
type EventDeleteNode struct {
	Node *api.Node
	// Checks is a list of functions to call to filter events for a watch
	// stream. They are applied with AND logic. They are only applicable for
	// calls to Watch.
	Checks []NodeCheckFunc
}

func (e EventDeleteNode) matches(watchEvent events.Event) bool {
	typedEvent, ok := watchEvent.(EventDeleteNode)
	if !ok {
		return false
	}

	for _, check := range e.Checks {
		if !check(e.Node, typedEvent.Node) {
			return false
		}
	}
	return true
}

// ClusterCheckFunc is the type of function used to perform filtering checks on
// api.Cluster structures.
type ClusterCheckFunc func(c1, c2 *api.Cluster) bool

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

// EventCreateCluster is the type used to put CreateCluster events on the
// publish/subscribe queue and filter these events in calls to Watch.
type EventCreateCluster struct {
	Cluster *api.Cluster
	// Checks is a list of functions to call to filter events for a watch
	// stream. They are applied with AND logic. They are only applicable for
	// calls to Watch.
	Checks []ClusterCheckFunc
}

func (e EventCreateCluster) matches(watchEvent events.Event) bool {
	typedEvent, ok := watchEvent.(EventCreateCluster)
	if !ok {
		return false
	}

	for _, check := range e.Checks {
		if !check(e.Cluster, typedEvent.Cluster) {
			return false
		}
	}
	return true
}

// EventUpdateCluster is the type used to put UpdateCluster events on the
// publish/subscribe queue and filter these events in calls to Watch.
type EventUpdateCluster struct {
	Cluster *api.Cluster
	// Checks is a list of functions to call to filter events for a watch
	// stream. They are applied with AND logic. They are only applicable for
	// calls to Watch.
	Checks []ClusterCheckFunc
}

func (e EventUpdateCluster) matches(watchEvent events.Event) bool {
	typedEvent, ok := watchEvent.(EventUpdateCluster)
	if !ok {
		return false
	}

	for _, check := range e.Checks {
		if !check(e.Cluster, typedEvent.Cluster) {
			return false
		}
	}
	return true
}

// EventDeleteCluster is the type used to put DeleteCluster events on the
// publish/subscribe queue and filter these events in calls to Watch.
type EventDeleteCluster struct {
	Cluster *api.Cluster
	// Checks is a list of functions to call to filter events for a watch
	// stream. They are applied with AND logic. They are only applicable for
	// calls to Watch.
	Checks []ClusterCheckFunc
}

func (e EventDeleteCluster) matches(watchEvent events.Event) bool {
	typedEvent, ok := watchEvent.(EventDeleteCluster)
	if !ok {
		return false
	}

	for _, check := range e.Checks {
		if !check(e.Cluster, typedEvent.Cluster) {
			return false
		}
	}
	return true
}

// SecretCheckFunc is the type of function used to perform filtering checks on
// api.Secret structures.
type SecretCheckFunc func(s1, s2 *api.Secret) bool

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

// EventCreateSecret is the type used to put CreateSecret events on the
// publish/subscribe queue and filter these events in calls to Watch.
type EventCreateSecret struct {
	Secret *api.Secret
	// Checks is a list of functions to call to filter events for a watch
	// stream. They are applied with AND logic. They are only applicable for
	// calls to Watch.
	Checks []SecretCheckFunc
}

func (e EventCreateSecret) matches(watchEvent events.Event) bool {
	typedEvent, ok := watchEvent.(EventCreateSecret)
	if !ok {
		return false
	}

	for _, check := range e.Checks {
		if !check(e.Secret, typedEvent.Secret) {
			return false
		}
	}
	return true
}

// EventUpdateSecret is the type used to put UpdateSecret events on the
// publish/subscribe queue and filter these events in calls to Watch.
type EventUpdateSecret struct {
	Secret *api.Secret
	// Checks is a list of functions to call to filter events for a watch
	// stream. They are applied with AND logic. They are only applicable for
	// calls to Watch.
	Checks []SecretCheckFunc
}

func (e EventUpdateSecret) matches(watchEvent events.Event) bool {
	typedEvent, ok := watchEvent.(EventUpdateSecret)
	if !ok {
		return false
	}

	for _, check := range e.Checks {
		if !check(e.Secret, typedEvent.Secret) {
			return false
		}
	}
	return true
}

// EventDeleteSecret is the type used to put DeleteSecret events on the
// publish/subscribe queue and filter these events in calls to Watch.
type EventDeleteSecret struct {
	Secret *api.Secret
	// Checks is a list of functions to call to filter events for a watch
	// stream. They are applied with AND logic. They are only applicable for
	// calls to Watch.
	Checks []SecretCheckFunc
}

func (e EventDeleteSecret) matches(watchEvent events.Event) bool {
	typedEvent, ok := watchEvent.(EventDeleteSecret)
	if !ok {
		return false
	}

	for _, check := range e.Checks {
		if !check(e.Secret, typedEvent.Secret) {
			return false
		}
	}
	return true
}

// ResourceCheckFunc is the type of function used to perform filtering checks on
// api.Resource structures.
type ResourceCheckFunc func(v1, v2 *api.Resource) bool

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

// EventCreateResource is the type used to put CreateResource events on the
// publish/subscribe queue and filter these events in calls to Watch.
type EventCreateResource struct {
	Resource *api.Resource
	// Checks is a list of functions to call to filter events for a watch
	// stream. They are applied with AND logic. They are only applicable for
	// calls to Watch.
	Checks []ResourceCheckFunc
}

func (e EventCreateResource) matches(watchEvent events.Event) bool {
	typedEvent, ok := watchEvent.(EventCreateResource)
	if !ok {
		return false
	}

	for _, check := range e.Checks {
		if !check(e.Resource, typedEvent.Resource) {
			return false
		}
	}
	return true
}

// EventUpdateResource is the type used to put UpdateResource events on the
// publish/subscribe queue and filter these events in calls to Watch.
type EventUpdateResource struct {
	Resource *api.Resource
	// Checks is a list of functions to call to filter events for a watch
	// stream. They are applied with AND logic. They are only applicable for
	// calls to Watch.
	Checks []ResourceCheckFunc
}

func (e EventUpdateResource) matches(watchEvent events.Event) bool {
	typedEvent, ok := watchEvent.(EventUpdateResource)
	if !ok {
		return false
	}

	for _, check := range e.Checks {
		if !check(e.Resource, typedEvent.Resource) {
			return false
		}
	}
	return true
}

// EventDeleteResource is the type used to put DeleteResource events on the
// publish/subscribe queue and filter these events in calls to Watch.
type EventDeleteResource struct {
	Resource *api.Resource
	// Checks is a list of functions to call to filter events for a watch
	// stream. They are applied with AND logic. They are only applicable for
	// calls to Watch.
	Checks []ResourceCheckFunc
}

func (e EventDeleteResource) matches(watchEvent events.Event) bool {
	typedEvent, ok := watchEvent.(EventDeleteResource)
	if !ok {
		return false
	}

	for _, check := range e.Checks {
		if !check(e.Resource, typedEvent.Resource) {
			return false
		}
	}
	return true
}

// ExtensionCheckFunc is the type of function used to perform filtering checks
// on api.Extension structures.
type ExtensionCheckFunc func(v1, v2 *api.Extension) bool

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

// EventCreateExtension is the type used to put CreateExtension events on the
// publish/subscribe queue and filter these events in calls to Watch.
type EventCreateExtension struct {
	Extension *api.Extension
	// Checks is a list of functions to call to filter events for a watch
	// stream. They are applied with AND logic. They are only applicable for
	// calls to Watch.
	Checks []ExtensionCheckFunc
}

func (e EventCreateExtension) matches(watchEvent events.Event) bool {
	typedEvent, ok := watchEvent.(EventCreateExtension)
	if !ok {
		return false
	}

	for _, check := range e.Checks {
		if !check(e.Extension, typedEvent.Extension) {
			return false
		}
	}
	return true
}

// EventUpdateExtension is the type used to put UpdateExtension events on the
// publish/subscribe queue and filter these events in calls to Watch.
type EventUpdateExtension struct {
	Extension *api.Extension
	// Checks is a list of functions to call to filter events for a watch
	// stream. They are applied with AND logic. They are only applicable for
	// calls to Watch.
	Checks []ExtensionCheckFunc
}

func (e EventUpdateExtension) matches(watchEvent events.Event) bool {
	typedEvent, ok := watchEvent.(EventUpdateExtension)
	if !ok {
		return false
	}

	for _, check := range e.Checks {
		if !check(e.Extension, typedEvent.Extension) {
			return false
		}
	}
	return true
}

// EventDeleteExtension is the type used to put DeleteExtension events on the
// publish/subscribe queue and filter these events in calls to Watch.
type EventDeleteExtension struct {
	Extension *api.Extension
	// Checks is a list of functions to call to filter events for a watch
	// stream. They are applied with AND logic. They are only applicable for
	// calls to Watch.
	Checks []ExtensionCheckFunc
}

func (e EventDeleteExtension) matches(watchEvent events.Event) bool {
	typedEvent, ok := watchEvent.(EventDeleteExtension)
	if !ok {
		return false
	}

	for _, check := range e.Checks {
		if !check(e.Extension, typedEvent.Extension) {
			return false
		}
	}
	return true
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
func Watch(queue *watch.Queue, specifiers ...Event) (eventq chan events.Event, cancel func()) {
	if len(specifiers) == 0 {
		return queue.Watch()
	}
	return queue.CallbackWatch(Matcher(specifiers...))
}

// Matcher returns an events.Matcher that matches the specifiers with OR logic.
func Matcher(specifiers ...Event) events.MatcherFunc {
	return events.MatcherFunc(func(event events.Event) bool {
		for _, s := range specifiers {
			if s.matches(event) {
				return true
			}
		}
		return false
	})
}
