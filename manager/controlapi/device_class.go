package controlapi

import (
	"context"

	"github.com/docker/swarmkit/api"
)

// TODO(dperny): all of these methods are stubs so that I can compile and check
// what i've done so far

// GetDeviceClass is a stub
func (s *Server) GetDeviceClass(ctx context.Context, request *api.GetDeviceClassRequest) (*api.GetDeviceClassResponse, error) {
	return nil, nil
}

// UpdateDeviceClass is a stub
func (s *Server) UpdateDeviceClass(ctx context.Context, request *api.UpdateDeviceClassRequest) (*api.UpdateDeviceClassResponse, error) {
	return nil, nil
}

// ListDeviceClasses is a stub
func (s *Server) ListDeviceClasses(ctx context.Context, request *api.ListDeviceClassesRequest) (*api.ListDeviceClassesResponse, error) {
	return nil, nil
}

// CreateDeviceClass is a stub
func (s *Server) CreateDeviceClass(ctx context.Context, request *api.CreateDeviceClassRequest) (*api.CreateDeviceClassResponse, error) {
	return nil, nil
}

// RemoveDeviceClass is a stub
func (s *Server) RemoveDeviceClass(ctx context.Context, request *api.RemoveDeviceClassRequest) (*api.RemoveDeviceClassResponse, error) {
	return nil, nil
}
