package task

import (
	"fmt"
	"os"
	"sort"
	"text/tabwriter"

	"github.com/moby/swarmkit/swarmd/cmd/swarmctl/common"
	"github.com/moby/swarmkit/v2/api"
)

type tasksBySlot []*api.Task

func (t tasksBySlot) Len() int {
	return len(t)
}
func (t tasksBySlot) Swap(i, j int) {
	t[i], t[j] = t[j], t[i]
}
func (t tasksBySlot) Less(i, j int) bool {
	// Sort by slot.
	if t[i].Slot != t[j].Slot {
		return t[i].Slot < t[j].Slot
	}

	// If same slot, sort by most recent.
	it := t[i].Meta.CreatedAt.AsTime()
	jt := t[j].Meta.CreatedAt.AsTime()
	return jt.Before(it)
}

// Print prints a list of tasks.
func Print(tasks []*api.Task, all bool, res *common.Resolver) {
	w := tabwriter.NewWriter(os.Stdout, 4, 4, 4, ' ', 0)
	defer w.Flush()

	common.PrintHeader(w, "Task ID", "Service", "Slot", "Image", "Desired State", "Last State", "Node")
	sort.Stable(tasksBySlot(tasks))
	for _, t := range tasks {
		if !all && t.DesiredState > api.TaskState_RUNNING {
			continue
		}
		c := t.Spec.GetContainer()
		fmt.Fprintf(w, "%s\t%s\t%d\t%s\t%s\t%s %s\t%s\n",
			t.Id,
			t.GetServiceAnnotations().GetName(),
			t.Slot,
			c.Image,
			t.DesiredState.String(),
			t.Status.GetState().String(),
			common.TimestampAgo(t.Status.GetTimestamp()),
			res.Resolve(api.Node{}, t.NodeId),
		)
	}
}
