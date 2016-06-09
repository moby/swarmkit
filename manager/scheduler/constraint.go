package scheduler

import (
	"strings"

	"github.com/docker/docker/pkg/stringid"
	"github.com/docker/swarmkit/api"
)

// ConstraintFilter selects only nodes that match certain labels.
type ConstraintFilter struct {
	constraints []Expr
}

// SetTask returns true when the filter is enable for a given task.
func (f *ConstraintFilter) SetTask(t *api.Task) bool {
	if t.Spec.Placement != nil && len(t.Spec.Placement.Constraints) > 0 {
		constraints, err := ParseExprs(t.Spec.Placement.Constraints)
		if err == nil {
			f.constraints = constraints
			return true
		}
	}
	return false
}

// Check returns true if the task's constraint is supported by the given node.
func (f *ConstraintFilter) Check(n *NodeInfo) bool {
	for _, constraint := range f.constraints {
		switch constraint.Key {
		case "node.id":
			// it could be full ID or TruncateID
			if !constraint.Match(n.ID) && !constraint.Match(stringid.TruncateID(n.ID)) {
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

			// node has labels from node.Spec.Annotations.Labels and node.Description.Engine.Labels
			// if a label exists on both, node.Spec.Annotations.Labels has precedence
			combinedLabels := make(map[string]string)
			if n.Description != nil && n.Description.Engine != nil && n.Description.Engine.Labels != nil {
				for k, v := range n.Description.Engine.Labels {
					combinedLabels[k] = v
				}
			}
			if n.Spec.Annotations.Labels != nil {
				for k, v := range n.Spec.Annotations.Labels {
					combinedLabels[k] = v
				}
			}

			label := constraint.Key[len("node.labels."):]
			// if the node doesn't have a specific label, val is an empty string
			// 'node.labels.key!=value' passes and
			// 'node.labels.key==value' fails
			val := combinedLabels[label]
			if !constraint.Match(val) {
				return false
			}
		}
	}

	return true
}
