package controlapi

import (
	"context"
	"strings"

	"github.com/docker/swarmkit/api"
	"github.com/docker/swarmkit/identity"
	"github.com/docker/swarmkit/manager/state/store"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// TODO(dperny): all of these methods are stubs so that I can compile and check
// what i've done so far

// GetDeviceClass returns a `GetDeviceClassResponse` with a `DeviceClass` with
// the same id as `GetDeviceClassRequest.DeviceClassID`
// - Returns `NotFound` if the DeviceClass with the given id is not found.
// - Returns `InvalidArgument` if the `GetDeviceClass.DeviceClassID` is empty.
// - Returns an error if getting fails.
func (s *Server) GetDeviceClass(ctx context.Context, request *api.GetDeviceClassRequest) (*api.GetDeviceClassResponse, error) {
	if request.DeviceClassID == "" {
		return nil, status.Errorf(codes.InvalidArgument, "device class ID must be provided")
	}

	var deviceClass *api.DeviceClass
	s.store.View(func(tx store.ReadTx) {
		deviceClass = store.GetDeviceClass(tx, request.DeviceClassID)
	})

	if deviceClass == nil {
		return nil, status.Errorf(
			codes.NotFound,
			"device class %s was not found",
			request.DeviceClassID,
		)
	}
	return &api.GetDeviceClassResponse{DeviceClass: deviceClass}, nil
}

// UpdateDeviceClass updates the DeviceClass referenced by the DeviceClassID
// with the given DeviceClassSpec
func (s *Server) UpdateDeviceClass(ctx context.Context, request *api.UpdateDeviceClassRequest) (*api.UpdateDeviceClassResponse, error) {
	if request.DeviceClassID == "" || request.DeviceClassVersion == nil {
		return nil, status.Errorf(codes.InvalidArgument, errInvalidArgument.Error())
	}

	var deviceClass *api.DeviceClass
	// we must do the entire update procedure inside of an update block,
	// because other updates must not proceed in the meantime.
	if err := s.store.Update(func(tx store.Tx) error {
		deviceClass = store.GetDeviceClass(tx, request.DeviceClassID)
		if deviceClass == nil {
			return status.Errorf(
				codes.NotFound,
				"device class %s was not found",
				request.DeviceClassID,
			)
		}

		if err := validateDeviceClassUpdate(&(deviceClass.Spec), request.Spec); err != nil {
			return err
		}

		// We only allow updating labels. Copy over the labels explicitly
		// instead of the whole spec so we don't end up accidentally
		// overwriting some field
		deviceClass.Meta.Version = *request.DeviceClassVersion
		deviceClass.Spec.Annotations.Labels = request.Spec.Annotations.Labels

		return store.UpdateDeviceClass(tx, deviceClass)
	}); err != nil {
		return nil, err
	}

	return &api.UpdateDeviceClassResponse{
		DeviceClass: deviceClass,
	}, nil
}

// ListDeviceClasses is a stub
func (s *Server) ListDeviceClasses(ctx context.Context, request *api.ListDeviceClassesRequest) (*api.ListDeviceClassesResponse, error) {
	var (
		deviceClasses     []*api.DeviceClass
		respDeviceClasses []*api.DeviceClass
		err               error
		byFilters         []store.By
		labels            map[string]string
	)

	if request.Filters != nil {
		for _, name := range request.Filters.Names {
			byFilters = append(byFilters, store.ByName(name))
		}
		for _, prefix := range request.Filters.NamePrefixes {
			byFilters = append(byFilters, store.ByNamePrefix(prefix))
		}
		for _, prefix := range request.Filters.IDPrefixes {
			byFilters = append(byFilters, store.ByIDPrefix(prefix))
		}
		labels = request.Filters.Labels
	}

	var by store.By
	switch len(byFilters) {
	case 0:
		by = store.All
	case 1:
		by = byFilters[0]
	default:
		by = store.Or(byFilters...)
	}

	s.store.View(func(tx store.ReadTx) {
		deviceClasses, err = store.FindDeviceClasses(tx, by)
	})
	if err != nil {
		return nil, err
	}

	for _, deviceClass := range deviceClasses {
		if !filterMatchLabels(deviceClass.Spec.Annotations.Labels, labels) {
			continue
		}
		respDeviceClasses = append(respDeviceClasses, deviceClass)
	}
	return &api.ListDeviceClassesResponse{DeviceClasses: respDeviceClasses}, nil
}

// CreateDeviceClass creates and returns a DeviceClass based on the provided
// DeviceClassSpec.
func (s *Server) CreateDeviceClass(ctx context.Context, request *api.CreateDeviceClassRequest) (*api.CreateDeviceClassResponse, error) {
	// build the object
	deviceClass := &api.DeviceClass{
		ID:   identity.NewID(),
		Spec: *(request.Spec),
	}
	if err := validateDeviceClass(&(deviceClass.Spec)); err != nil {
		return nil, err
	}
	if err := s.store.Update(func(tx store.Tx) error {
		return store.CreateDeviceClass(tx, deviceClass)
	}); err != nil {
		// special case ErrNameConflict, to correctly wrap it in a proper gRPC
		// error type
		if err == store.ErrNameConflict {
			return nil, status.Errorf(
				codes.AlreadyExists,
				"device class with name %v already exists",
				deviceClass.Spec.Annotations.Name,
			)
		}

		// otherwise, just return whatever we get
		return nil, err
	}

	return &api.CreateDeviceClassResponse{
		DeviceClass: deviceClass,
	}, nil
}

// RemoveDeviceClass removes the DeviceClass referenced by
// `RemoveDeviceClassRequest.ID`.
// - Returns `InvalidArgument` if `RemoveDeviceClassRequest.ID` is empty.
// - Returns `NotFound` if the a deviceclass named
//   `RemoveDeviceClassRequest.ID` is not found.
// - Returns `DeviceClassInUse` if the DeviceClass is currently in use
// - Returns an error if the deletion fails.
func (s *Server) RemoveDeviceClass(ctx context.Context, request *api.RemoveDeviceClassRequest) (*api.RemoveDeviceClassResponse, error) {
	if request.DeviceClassID == "" {
		return nil, status.Errorf(codes.InvalidArgument, "device class ID must be provided")
	}

	err := s.store.Update(func(tx store.Tx) error {
		deviceClass := store.GetDeviceClass(tx, request.DeviceClassID)
		if deviceClass == nil {
			return status.Errorf(
				codes.NotFound,
				"could not find device class %v", request.DeviceClassID,
			)
		}

		// check if any nodes currently have devices of this class
		// we'll keep a slice of all the nodeIDs that this device class is in
		// use by to make it easier for the operator to know what they've
		// missed
		nodeIDs := []string{}
		nodes, err := store.FindNodes(tx, store.All)
		if err != nil {
			return status.Errorf(
				codes.Internal,
				"could not find nodes to check if device class is in use: %v",
				err,
			)
		}

		for _, node := range nodes {
			for _, device := range node.Spec.Devices {
				if device.DeviceClassID == request.DeviceClassID {
					nodeIDs = append(nodeIDs, node.ID)
					// break out of the devices loop, so we check the next node
					break
				}
			}
		}

		if len(nodeIDs) != 0 {
			return status.Errorf(
				codes.InvalidArgument,
				"device class '%s' is in use on the following nodes: %v",
				request.DeviceClassID, strings.Join(nodeIDs, ", "),
			)
		}
		return store.DeleteDeviceClass(tx, request.DeviceClassID)
	})

	switch err {
	case store.ErrNotExist:
		return nil, status.Errorf(
			codes.NotFound, "device class %v not found", request.DeviceClassID,
		)
	case nil:
		return &api.RemoveDeviceClassResponse{}, nil
	default:
		return nil, err
	}
}

// validateDeviceClass checks that the the provided DeviceClassSpec is free
// from obvious errors
//
// Currently, because the DeviceClassSpec is almost empty, this does very
// little, but it has been provided for completeness and future-proofing
func validateDeviceClass(spec *api.DeviceClassSpec) error {
	// check first if the spec is nil.
	if spec == nil {
		return status.Errorf(codes.InvalidArgument, "must provide a spec")
	}
	// now, we have to check the annotations. this is common for every swarmkit
	// object
	if err := validateAnnotations(spec.Annotations); err != nil {
		return err
	}
	return nil
}

// validateDeviceClassUpdate validates that the given update can be proceeded
// with. It returns an error if the user is attempting to update a disallowed
// field.
func validateDeviceClassUpdate(old, new *api.DeviceClassSpec) error {
	// first, validate the new spec is valid on its own
	if err := validateDeviceClass(new); err != nil {
		return err
	}

	// next, validate that the user has not changed the name or shared status
	if old.Annotations.Name != new.Annotations.Name || old.Shared != new.Shared {
		return status.Errorf(
			codes.InvalidArgument, "only updates to labels are allowed",
		)
	}

	return nil
}
