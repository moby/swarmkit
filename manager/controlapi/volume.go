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

	if err := validateDriver(spec.DriverConfiguration); err != nil {
		return err
	}

	return nil
}

// CreateVolume creates and return a Volume based on the provided VolumeSpec.
// - Returns `InvalidArgument` if the VolumeSpec is malformed.
// - Returns `Unimplemented` if the VolumeSpec references unimplemented features.
// - Returns `AlreadyExists` if the VolumeID conflicts.
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

// GetVolume returns a Volume given a VolumeID.
// - Returns `InvalidArgument` if VolumeID is not provided.
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

// RemoveVolume removes a Volume referenced by VolumeID.
// - Returns `InvalidArgument` if VolumeID is not provided.
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

// ListVolumes returns a list of all volumes.
func (s *Server) ListVolumes(ctx context.Context, request *api.ListVolumesRequest) (*api.ListVolumesResponse, error) {
	var (
		volumes []*api.Volume
		err     error
	)
	s.store.View(func(tx store.ReadTx) {
		volumes, err = store.FindVolumes(tx, store.All)
	})
	if err != nil {
		return nil, err
	}
	return &api.ListVolumesResponse{
		Volumes: volumes,
	}, nil
}
