package task

import (
	"fmt"
	"os"
	"sort"
	"text/tabwriter"

	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/cmd/swarmctl/common"
	"github.com/docker/swarm-v2/protobuf/ptypes"
)

type tasksByInstance []*api.Task

func (t tasksByInstance) Len() int {
	return len(t)
}
func (t tasksByInstance) Swap(i, j int) {
	t[i], t[j] = t[j], t[i]
}
func (t tasksByInstance) Less(i, j int) bool {
	// Sort by instance.
	if t[i].Instance != t[j].Instance {
		return t[i].Instance < t[j].Instance
	}

	// If same instance, sort by most recent.
	// TODO(aluzzardi): This should actually be task creation
	// time rather than task status.
	it, err := ptypes.Timestamp(t[i].Status.Timestamp)
	if err != nil {
		panic(err)
	}
	jt, err := ptypes.Timestamp(t[j].Status.Timestamp)
	if err != nil {
		panic(err)
	}
	return jt.Before(it)
}

// Print prints a list of tasks.
func Print(tasks []*api.Task, all bool, res *common.Resolver) {
	w := tabwriter.NewWriter(os.Stdout, 4, 4, 4, ' ', 0)
	defer w.Flush()

	common.PrintHeader(w, "Task ID", "Instance", "Image", "Desired State", "Last State", "Node")
	sort.Stable(tasksByInstance(tasks))
	for _, t := range tasks {
		if !all && t.DesiredState > api.TaskStateRunning {
			continue
		}
		c := t.GetContainer().Spec
		fmt.Fprintf(w, "%s\t%s.%d\t%s\t%s\t%s %s\t%s\n",
			t.ID,
			t.Annotations.Name,
			t.Instance,
			c.Image.Reference,
			t.DesiredState.String(),
			t.Status.State.String(),
			common.TimestampAgo(t.Status.Timestamp),
			res.Resolve(api.Node{}, t.NodeID),
		)
	}
}
