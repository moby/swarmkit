package controlapi

import (
	"context"
	"errors"

	"github.com/docker/swarmkit/api"
)

// CreateResource returns a `CreateResourceResponse` after creating a `Resource` based
// on the provided `CreateResourceRequest.Resource`.
// - Returns `InvalidArgument` if the `CreateResourceRequest.Resource` is malformed,
//   or if the config data is too long or contains invalid characters.
// - Returns an error if the creation fails.
func (s *Server) CreateResource(ctx context.Context, request *api.CreateResourceRequest) (*api.CreateResourceResponse, error) {
	return nil, errors.New("not implemented")
}

// GetResource returns a `GetResourceResponse` with a `Resource` with the same
// id as `GetResourceRequest.Resource`
// - Returns `NotFound` if the Resource with the given id is not found.
// - Returns `InvalidArgument` if the `GetResourceRequest.Resource` is empty.
// - Returns an error if getting fails.
func (s *Server) GetResource(ctx context.Context, request *api.GetResourceRequest) (*api.GetResourceResponse, error) {
	return nil, errors.New("not implemented")
}

// RemoveResource removes the `Resource` referenced by `RemoveResourceRequest.ResourceID`.
// - Returns `InvalidArgument` if `RemoveResourceRequest.ResourceID` is empty.
// - Returns `NotFound` if the a resource named `RemoveResourceRequest.ResourceID` is not found.
// - Returns an error if the deletion fails.
func (s *Server) RemoveResource(ctx context.Context, request *api.RemoveResourceRequest) (*api.RemoveResourceResponse, error) {
	return nil, errors.New("not implemented")
}

// ListResources returns a `ListResourcesResponse` with a list of `Resource`s stored in the raft store,
// or all resources matching any name in `ListConfigsRequest.Names`, any
// name prefix in `ListResourcesRequest.NamePrefixes`, any id in
// `ListResourcesRequest.ResourceIDs`, or any id prefix in `ListResourcesRequest.IDPrefixes`.
// - Returns an error if listing fails.
func (s *Server) ListResources(ctx context.Context, request *api.ListResourcesRequest) (*api.ListResourcesResponse, error) {
	return nil, errors.New("not implemented")
}

// UpdateResource updates the resource with the given `UpdateResourceRequest.Resource.Id` using the given `UpdateResourceRequest.Resource` and returns a `UpdateResourceResponse`.
// - Returns `NotFound` if the Resource with the given `UpdateResourceRequest.Resource.Id` is not found.
// - Returns `InvalidArgument` if the UpdateResourceRequest.Resource.Id` is empty.
// - Returns an error if updating fails.
func (s *Server) UpdateResource(ctx context.Context, request *api.UpdateResourceRequest) (*api.UpdateResourceResponse, error) {
	return nil, errors.New("not implemented")
}
