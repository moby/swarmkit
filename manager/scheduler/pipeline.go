package scheduler

import "github.com/docker/swarm-v2/api"

var (
	defaultFilters = []Filter{
		// Always check for readiness first.
		&ReadyFilter{},
		&ResourceFilter{},
		&PluginFilter{},
		&ConstraintFilter{},
	}
)

// Pipeline runs a set of filters against nodes.
type Pipeline struct {
	task      *api.Task
	checklist []Filter
}

// NewPipeline returns a pipeline with the default set of filters.
func NewPipeline(t *api.Task) *Pipeline {
	p := &Pipeline{
		task:      t,
		checklist: []Filter{},
	}

	// FIXME(aaronl): This is quite alloc-heavy. It may be better to
	// redesign Pipeline so it doesn't require allocating a separate slice
	// for each task. Maybe it could use a bitfield to specify which
	// filters are enabled.
	for _, f := range defaultFilters {
		if f.Enabled(t) {
			p.checklist = append(p.checklist, f)
		}
	}

	return p
}

// Process a node through the filter pipeline.
// Returns true if all filters pass, false otherwise.
func (p *Pipeline) Process(n *NodeInfo) bool {
	for _, f := range p.checklist {
		if !f.Check(p.task, n) {
			// Immediately stop on first failure.
			return false
		}
	}
	return true
}
