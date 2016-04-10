package filter

import "github.com/docker/swarm-v2/api"

var (
	defaultFilters = []Filter{
		// Always check for readiness first.
		&ReadyFilter{},
		&ResourceFilter{},
	}
)

type Pipeline struct {
	task      *api.Task
	checklist []Filter
}

func NewPipeline(t *api.Task) *Pipeline {
	p := &Pipeline{
		task:      t,
		checklist: []Filter{},
	}

	for _, f := range defaultFilters {
		if f.Enabled(t) {
			p.checklist = append(p.checklist, f)
		}
	}

	return p
}

// Process a node through the filter pipeline.
// Returns true if all filters pass, false otherwise.
func (p *Pipeline) Process(n *api.Node) bool {
	for _, f := range p.checklist {
		if !f.Check(p.task, n) {
			// Immediately stop on first failure.
			return false
		}
	}
	return true
}
