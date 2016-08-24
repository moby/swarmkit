package controlapi

import (
	"fmt"
	"net"

	"github.com/docker/docker/pkg/plugingetter"
	"github.com/docker/libnetwork/driverapi"
	"github.com/docker/libnetwork/ipamapi"
	"github.com/docker/swarmkit/api"
	"github.com/docker/swarmkit/identity"
	"github.com/docker/swarmkit/manager/state/store"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
)

func validateIPAMConfiguration(ipamConf *api.IPAMConfig) (me MultiError) {
	if ipamConf == nil {
		err := grpc.Errorf(codes.InvalidArgument, "ipam configuration: cannot be empty")
		me = append(me, err)
		return
	}

	_, subnet, err := net.ParseCIDR(ipamConf.Subnet)
	if err != nil {
		err = grpc.Errorf(codes.InvalidArgument, "ipam configuration: invalid subnet %s", ipamConf.Subnet)
		me = append(me, err)
	}

	if ipamConf.Range != "" {
		ip, _, err := net.ParseCIDR(ipamConf.Range)
		if err != nil {
			err = grpc.Errorf(codes.InvalidArgument, "ipam configuration: invalid range %s", ipamConf.Range)
			me = append(me, err)
		} else if !subnet.Contains(ip) {
			err = grpc.Errorf(codes.InvalidArgument, "ipam configuration: subnet %s does not contain range %s", ipamConf.Subnet, ipamConf.Range)
			me = append(me, err)
		}
	}

	if ipamConf.Gateway != "" {
		ip := net.ParseIP(ipamConf.Gateway)
		if ip == nil {
			err = grpc.Errorf(codes.InvalidArgument, "ipam configuration: invalid gateway %s", ipamConf.Gateway)
			me = append(me, err)
		} else if !subnet.Contains(ip) {
			err = grpc.Errorf(codes.InvalidArgument, "ipam configuration: subnet %s does not contain gateway %s", ipamConf.Subnet, ipamConf.Gateway)
			me = append(me, err)
		}
	}

	return
}

<<<<<<< HEAD
func validateIPAM(ipam *api.IPAMOptions, pg plugingetter.PluginGetter) error {
=======
func validateIPAM(ipam *api.IPAMOptions) (me MultiError) {
>>>>>>> return all validation error in spec
	if ipam == nil {
		// It is ok to not specify any IPAM configurations. We
		// will choose good defaults.
		return nil
	}

<<<<<<< HEAD
	if err := validateDriver(ipam.Driver, pg, ipamapi.PluginEndpointType); err != nil {
		return err
=======
	if err := validateDriver(ipam.Driver); err != nil {
		me = append(me, err)
	}

	if ipam.Driver != nil && ipam.Driver.Name != ipamapi.DefaultIPAM {
		err := grpc.Errorf(codes.InvalidArgument, "invalid IPAM specified")
		me = append(me, err)
>>>>>>> return all validation error in spec
	}

	for _, ipamConf := range ipam.Configs {
		if err := validateIPAMConfiguration(ipamConf); err != nil {
			me = append(me, err)
		}
	}

	return
}

<<<<<<< HEAD
func validateNetworkSpec(spec *api.NetworkSpec, pg plugingetter.PluginGetter) error {
=======
func validateNetworkSpec(spec *api.NetworkSpec) (me MultiError) {
>>>>>>> return all validation error in spec
	if spec == nil {
		err := grpc.Errorf(codes.InvalidArgument, errInvalidArgument.Error())
		me = append(me, err)
		return
	}

	if err := validateAnnotations(spec.Annotations); err != nil {
		me = append(me, err)
	}

<<<<<<< HEAD
	if err := validateDriver(spec.DriverConfig, pg, driverapi.NetworkPluginEndpointType); err != nil {
		return err
	}

	if err := validateIPAM(spec.IPAM, pg); err != nil {
		return err
=======
	if err := validateDriver(spec.DriverConfig); err != nil {
		me = append(me, err)
	}

	if spec.DriverConfig != nil && spec.DriverConfig.Name != networkallocator.DefaultDriver {
		err := grpc.Errorf(codes.InvalidArgument, "invalid driver specified")
		me = append(me, err)
	}

	if err := validateIPAM(spec.IPAM); err != nil {
		me = append(me, err)
>>>>>>> return all validation error in spec
	}

	return
}

// CreateNetwork creates and returns a Network based on the provided NetworkSpec.
// - Returns `InvalidArgument` if the NetworkSpec is malformed.
// - Returns an error if the creation fails.
func (s *Server) CreateNetwork(ctx context.Context, request *api.CreateNetworkRequest) (*api.CreateNetworkResponse, error) {
	// if you change this function, you have to change createInternalNetwork in
	// the tests to match it (except the part where we check the label).
	if err := validateNetworkSpec(request.Spec, s.pg); err != nil {
		return nil, err
	}

	if _, ok := request.Spec.Annotations.Labels["com.docker.swarm.internal"]; ok {
		return nil, grpc.Errorf(codes.PermissionDenied, "label com.docker.swarm.internal is for predefined internal networks and cannot be applied by users")
	}

	// TODO(mrjana): Consider using `Name` as a primary key to handle
	// duplicate creations. See #65
	n := &api.Network{
		ID:   identity.NewID(),
		Spec: *request.Spec,
	}

	err := s.store.Update(func(tx store.Tx) error {
		return store.CreateNetwork(tx, n)
	})
	if err != nil {
		return nil, err
	}

	return &api.CreateNetworkResponse{
		Network: n,
	}, nil
}

// GetNetwork returns a Network given a NetworkID.
// - Returns `InvalidArgument` if NetworkID is not provided.
// - Returns `NotFound` if the Network is not found.
func (s *Server) GetNetwork(ctx context.Context, request *api.GetNetworkRequest) (*api.GetNetworkResponse, error) {
	if request.NetworkID == "" {
		return nil, grpc.Errorf(codes.InvalidArgument, errInvalidArgument.Error())
	}

	var n *api.Network
	s.store.View(func(tx store.ReadTx) {
		n = store.GetNetwork(tx, request.NetworkID)
	})
	if n == nil {
		return nil, grpc.Errorf(codes.NotFound, "network %s not found", request.NetworkID)
	}
	return &api.GetNetworkResponse{
		Network: n,
	}, nil
}

// RemoveNetwork removes a Network referenced by NetworkID.
// - Returns `InvalidArgument` if NetworkID is not provided.
// - Returns `NotFound` if the Network is not found.
// - Returns an error if the deletion fails.
func (s *Server) RemoveNetwork(ctx context.Context, request *api.RemoveNetworkRequest) (*api.RemoveNetworkResponse, error) {
	if request.NetworkID == "" {
		return nil, grpc.Errorf(codes.InvalidArgument, errInvalidArgument.Error())
	}

	err := s.store.Update(func(tx store.Tx) error {
		services, err := store.FindServices(tx, store.ByReferencedNetworkID(request.NetworkID))
		if err != nil {
			return grpc.Errorf(codes.Internal, "could not find services using network %s: %v", request.NetworkID, err)
		}

		if len(services) != 0 {
			return grpc.Errorf(codes.FailedPrecondition, "network %s is in use by service %s", request.NetworkID, services[0].ID)
		}

		tasks, err := store.FindTasks(tx, store.ByReferencedNetworkID(request.NetworkID))
		if err != nil {
			return grpc.Errorf(codes.Internal, "could not find tasks using network %s: %v", request.NetworkID, err)
		}

		if len(tasks) != 0 {
			return grpc.Errorf(codes.FailedPrecondition, "network %s is in use by task %s", request.NetworkID, tasks[0].ID)
		}

		nw := store.GetNetwork(tx, request.NetworkID)
		if _, ok := nw.Spec.Annotations.Labels["com.docker.swarm.internal"]; ok {
			networkDescription := nw.ID
			if nw.Spec.Annotations.Name != "" {
				networkDescription = fmt.Sprintf("%s (%s)", nw.Spec.Annotations.Name, nw.ID)
			}
			return grpc.Errorf(codes.PermissionDenied, "%s is a pre-defined network and cannot be removed", networkDescription)
		}
		return store.DeleteNetwork(tx, request.NetworkID)
	})
	if err != nil {
		if err == store.ErrNotExist {
			return nil, grpc.Errorf(codes.NotFound, "network %s not found", request.NetworkID)
		}
		return nil, err
	}
	return &api.RemoveNetworkResponse{}, nil
}

func filterNetworks(candidates []*api.Network, filters ...func(*api.Network) bool) []*api.Network {
	result := []*api.Network{}

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

// ListNetworks returns a list of all networks.
func (s *Server) ListNetworks(ctx context.Context, request *api.ListNetworksRequest) (*api.ListNetworksResponse, error) {
	var (
		networks []*api.Network
		err      error
	)

	s.store.View(func(tx store.ReadTx) {
		switch {
		case request.Filters != nil && len(request.Filters.Names) > 0:
			networks, err = store.FindNetworks(tx, buildFilters(store.ByName, request.Filters.Names))
		case request.Filters != nil && len(request.Filters.NamePrefixes) > 0:
			networks, err = store.FindNetworks(tx, buildFilters(store.ByNamePrefix, request.Filters.NamePrefixes))
		case request.Filters != nil && len(request.Filters.IDPrefixes) > 0:
			networks, err = store.FindNetworks(tx, buildFilters(store.ByIDPrefix, request.Filters.IDPrefixes))
		default:
			networks, err = store.FindNetworks(tx, store.All)
		}
	})
	if err != nil {
		return nil, err
	}

	if request.Filters != nil {
		networks = filterNetworks(networks,
			func(e *api.Network) bool {
				return filterContains(e.Spec.Annotations.Name, request.Filters.Names)
			},
			func(e *api.Network) bool {
				return filterContainsPrefix(e.Spec.Annotations.Name, request.Filters.NamePrefixes)
			},
			func(e *api.Network) bool {
				return filterContainsPrefix(e.ID, request.Filters.IDPrefixes)
			},
			func(e *api.Network) bool {
				return filterMatchLabels(e.Spec.Annotations.Labels, request.Filters.Labels)
			},
		)
	}

	return &api.ListNetworksResponse{
		Networks: networks,
	}, nil
}
