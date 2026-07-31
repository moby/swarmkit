package common

import (
	"context"
	"fmt"

	"github.com/moby/swarmkit/v2/api"
	"github.com/spf13/cobra"
)

// Resolver provides ID to Name resolution.
type Resolver struct {
	cmd   *cobra.Command
	c     api.ControlClient
	ctx   context.Context
	cache map[string]string
}

// NewResolver creates a new Resolver.
func NewResolver(cmd *cobra.Command, c api.ControlClient) *Resolver {
	return &Resolver{
		cmd:   cmd,
		c:     c,
		ctx:   Context(cmd),
		cache: make(map[string]string),
	}
}

func (r *Resolver) get(t any, id string) string {
	switch t.(type) {
	case api.Node:
		res, err := r.c.GetNode(r.ctx, &api.GetNodeRequest{NodeId: id})
		if err != nil {
			return id
		}
		if name := res.GetNode().GetSpec().GetAnnotations().GetName(); name != "" {
			return name
		}
		if res.GetNode().GetDescription() == nil {
			return id
		}
		return res.GetNode().GetDescription().GetHostname()
	case api.Service:
		res, err := r.c.GetService(r.ctx, &api.GetServiceRequest{ServiceId: id})
		if err != nil {
			return id
		}
		return res.GetService().GetSpec().GetAnnotations().GetName()
	case api.Task:
		res, err := r.c.GetTask(r.ctx, &api.GetTaskRequest{TaskId: id})
		if err != nil {
			return id
		}
		svc := r.get(api.Service{}, res.GetTask().GetServiceId())
		return fmt.Sprintf("%s.%d", svc, res.GetTask().GetSlot())
	default:
		return id
	}
}

// Resolve will attempt to resolve an ID to a Name by querying the manager.
// Results are stored into a cache.
// If the `-n` flag is used in the command-line, resolution is disabled.
func (r *Resolver) Resolve(t any, id string) string {
	if r.cmd.Flags().Changed("no-resolve") {
		return id
	}
	if name, ok := r.cache[id]; ok {
		return name
	}
	name := r.get(t, id)
	r.cache[id] = name
	return name
}
