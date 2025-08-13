package controlapi

import (
	"context"

	"github.com/moby/swarmkit/v2/api"
)

// ViewResponseMutator provides callbacks which may modify the response objects
// for Get or List Control API requests before they are sent to the client.
type ViewResponseMutator interface {
	OnGetNetwork(context.Context, *api.Network) error
	OnListNetworks(context.Context, []*api.Network) error
}

type NoopViewResponseMutator struct{}

func (NoopViewResponseMutator) OnGetNetwork(ctx context.Context, n *api.Network) error {
	return nil
}

func (NoopViewResponseMutator) OnListNetworks(ctx context.Context, networks []*api.Network) error {
	return nil
}
