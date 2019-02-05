package controlapi

import (
	"context"
	"errors"

	"github.com/docker/swarmkit/api"
)

// CreateExtension creates an `Extension` based on the provided `CreateExtensionRequest.Extension`
// and returns a `CreateExtensionResponse`.
// - Returns `InvalidArgument` if the `CreateExtensionRequest.Extension` is malformed,
//   or fails validation.
// - Returns an error if the creation fails.
func (s *Server) CreateExtension(ctx context.Context, request *api.CreateExtensionRequest) (*api.CreateExtensionResponse, error) {
	return nil, errors.New("not implemented")
}

// GetExtension returns a `GetExtensionResponse` with a `Extension` with the same
// id as `GetExtensionRequest.extension_id`
// - Returns `NotFound` if the Extension with the given id is not found.
// - Returns `InvalidArgument` if the `GetExtensionRequest.extension_id` is empty.
// - Returns an error if the get fails.
func (s *Server) GetExtension(ctx context.Context, request *api.GetExtensionRequest) (*api.GetExtensionResponse, error) {
	return nil, errors.New("not implemented")
}

// RemoveExtension removes the extension referenced by `RemoveExtensionRequest.ID`.
// - Returns `InvalidArgument` if `RemoveExtensionRequest.extension_id` is empty.
// - Returns `NotFound` if the an extension named `RemoveExtensionRequest.extension_id` is not found.
// - Returns an error if the deletion fails.
func (s *Server) RemoveExtension(ctx context.Context, request *api.RemoveExtensionRequest) (*api.RemoveExtensionResponse, error) {
	return nil, errors.New("not implemented")
}
