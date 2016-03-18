package common

import (
	"github.com/docker/swarm-v2/api"
	"github.com/spf13/cobra"
	"golang.org/x/net/context"
)

// Resolver provides ID to Name resolution.
type Resolver struct {
	cmd   *cobra.Command
	c     api.ClusterClient
	ctx   context.Context
	cache map[string]string
}

// NewResolver creates a new Resolver.
func NewResolver(cmd *cobra.Command, c api.ClusterClient) *Resolver {
	return &Resolver{
		cmd:   cmd,
		c:     c,
		ctx:   Context(cmd),
		cache: make(map[string]string),
	}
}

func (r *Resolver) get(t interface{}, id string) string {
	switch t.(type) {
	case api.Node:
		res, err := r.c.GetNode(r.ctx, &api.GetNodeRequest{NodeID: id})
		if err != nil {
			return id
		}
		if res.Node.Spec != nil && res.Node.Spec.Meta.Name != "" {
			return res.Node.Spec.Meta.Name
		}
		return res.Node.Description.Hostname
	case api.Job:
		res, err := r.c.GetJob(r.ctx, &api.GetJobRequest{JobID: id})
		if err != nil {
			return id
		}
		return res.Job.Spec.Meta.Name
	default:
		return id
	}
}

// Resolve will attempt to resolve an ID to a Name by querying the manager.
// Results are stored into a cache.
// If the `-n` flag is used in the command-line, resolution is disabled.
func (r *Resolver) Resolve(t interface{}, id string) string {
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

// LookupID attempts to resolve a Name into an ID.
// TODO(aluzzardi): This is a giant hack until we have server-side name lookups.
func LookupID(ctx context.Context, c api.ClusterClient, name string) string {
	r, err := c.ListNodes(ctx, &api.ListNodesRequest{})
	if err != nil {
		return name
	}
	for _, n := range r.Nodes {
		if n.Spec != nil && n.Spec.Meta.Name == name {
			return n.ID
		}
		if n.Description != nil && n.Description.Hostname == name {
			return n.ID
		}
	}
	return name
}
