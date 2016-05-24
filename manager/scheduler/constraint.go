package scheduler

import (
	"strings"

	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/spec"
)

// ConstraintFilter selects only nodes that mach certain labels.
type ConstraintFilter struct {
}

// Enabled returns true when the filter is enable for a given task.
func (f *ConstraintFilter) Enabled(t *api.Task) bool {
	container := t.GetContainer()
	if container == nil {
		return false
	}
	return container.Spec.Placement != nil && len(container.Spec.Placement.Constraints) > 0
}

// Check returns true if the task's constraint is supported by the given node.
func (f *ConstraintFilter) Check(t *api.Task, n *NodeInfo) bool {
	containerSpec := t.GetContainer().Spec
	// containerSpec.Placement is not nil because it's checked in Enabled()
	constraints, err := spec.ParseExprs(containerSpec.Placement.Constraints)
	// constraints are validated at service spec validation
	if err != nil {
		return true
	}

	for _, constraint := range constraints {
		switch constraint.Key {
		case "node.id":
			if !constraint.Match(n.ID) {
				return false
			}
		case "node.name":
			// if this node doesn't have hostname
			// it's equivalent to match an empty hostname
			// where '==' would fail, '!=' matches
			if n.Description == nil {
				if !constraint.Match("") {
					return false
				}
				continue
			}
			if !constraint.Match(n.Description.Hostname) {
				return false
			}
		default:
			// default is node label in form like 'node.labels.key==value'
			// if it is not well formed, always fails it
			if !strings.HasPrefix(constraint.Key, "node.labels.") {
				return false
			}
			// if the node doesn't have any label,
			// it's equivalent to match an empty value.
			// that is, 'node.labels.key!=value' should pass and
			// 'node.labels.key==value' should fail
			if n.Spec.Annotations.Labels == nil {
				if !constraint.Match("") {
					return false
				}
				continue
			}
			label := constraint.Key[len("node.labels."):]
			// if the node doesn't have this specific label,
			// val is an empty string
			val := n.Spec.Annotations.Labels[label]
			if !constraint.Match(val) {
				return false
			}
		}
	}

	return true
}
