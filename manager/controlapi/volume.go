package controlapi

import (
	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/identity"
	"github.com/docker/swarm-v2/manager/state/store"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
)

func validateVolumeSpec(spec *api.VolumeSpec) error {
	if spec == nil {
		return grpc.Errorf(codes.InvalidArgument, errInvalidArgument.Error())
	}

	if spec.DriverConfiguration.Name == "" || spec.Annotations.Name == "" {
		return grpc.Errorf(codes.InvalidArgument, errInvalidArgument.Error())
	}

	return nil
}

// CreateVolume creates and return a Volume based on the provided VolumeSpec.
// - Returns `InvalidArgument` if the VolumeSpec is malformed.
// - Returns `Unimplemented` if the VolumeSpec references unimplemented features.
// - Returns `AlreadyExists` if the ID conflicts.
// - Returns an error if the creation fails.
func (s *Server) CreateVolume(ctx context.Context, request *api.CreateVolumeRequest) (*api.CreateVolumeResponse, error) {
	if err := validateVolumeSpec(request.Spec); err != nil {
		return nil, err
	}
	// TODO(amitshukla): validate driver_configuration

	// TODO(amitshukla): Consider using `Name` as a primary key to handle
	// duplicate creations. See #65
	volume := &api.Volume{
		ID:   identity.NewID(),
		Spec: *request.Spec,
	}

	err := s.store.Update(func(tx store.Tx) error {
		return store.CreateVolume(tx, volume)
	})
	if err != nil {
		return nil, err
	}

	return &api.CreateVolumeResponse{
		Volume: volume,
	}, nil
}

// GetVolume returns a Volume given a ID.
// - Returns `InvalidArgument` if ID is not provided.
// - Returns `NotFound` if the Volume is not found.
func (s *Server) GetVolume(ctx context.Context, request *api.GetVolumeRequest) (*api.GetVolumeResponse, error) {
	if request.VolumeID == "" {
		return nil, grpc.Errorf(codes.InvalidArgument, errInvalidArgument.Error())
	}

	var volume *api.Volume
	s.store.View(func(tx store.ReadTx) {
		volume = store.GetVolume(tx, request.VolumeID)
	})
	if volume == nil {
		return nil, grpc.Errorf(codes.NotFound, "volume %s not found", request.VolumeID)
	}
	return &api.GetVolumeResponse{
		Volume: volume,
	}, nil
}

// RemoveVolume removes a Volume referenced by ID.
// - Returns `InvalidArgument` if ID is not provided.
// - Returns `NotFound` if the Volume is not found.
// - Returns an error if the deletion fails.
func (s *Server) RemoveVolume(ctx context.Context, request *api.RemoveVolumeRequest) (*api.RemoveVolumeResponse, error) {
	if request.VolumeID == "" {
		return nil, grpc.Errorf(codes.InvalidArgument, errInvalidArgument.Error())
	}

	err := s.store.Update(func(tx store.Tx) error {
		return store.DeleteVolume(tx, request.VolumeID)
	})
	if err != nil {
		if err == store.ErrNotExist {
			return nil, grpc.Errorf(codes.NotFound, "volume %s not found", request.VolumeID)
		}
		return nil, err
	}
	return &api.RemoveVolumeResponse{}, nil
}

func filterVolumes(candidates []*api.Volume, filters ...func(*api.Volume) bool) []*api.Volume {
	result := []*api.Volume{}

	for _, c := range candidates {
		match := true
		for _, f := range filters {
			if !f(c) {
				match = false
				break
			}
		}
		if match {
			result = append(result, c)
		}
	}

	return result
}

// ListVolumes returns a list of all volumes.
func (s *Server) ListVolumes(ctx context.Context, request *api.ListVolumesRequest) (*api.ListVolumesResponse, error) {
	var (
		volumes []*api.Volume
		err     error
	)
	s.store.View(func(tx store.ReadTx) {
		switch {
		case request.Filters != nil && len(request.Filters.Names) > 0:
			volumes, err = store.FindVolumes(tx, buildFilters(store.ByName, request.Filters.Names))
		case request.Filters != nil && len(request.Filters.IDPrefixes) > 0:
			volumes, err = store.FindVolumes(tx, buildFilters(store.ByIDPrefix, request.Filters.IDPrefixes))
		default:
			volumes, err = store.FindVolumes(tx, store.All)
		}
	})
	if err != nil {
		return nil, err
	}

	if request.Filters != nil {
		volumes = filterVolumes(volumes,
			func(e *api.Volume) bool {
				return filterContains(e.Spec.Annotations.Name, request.Filters.Names)
			},
			func(e *api.Volume) bool {
				return filterContainsPrefix(e.ID, request.Filters.IDPrefixes)
			},
		)
	}

	return &api.ListVolumesResponse{
		Volumes: volumes,
	}, nil
}
