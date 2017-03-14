package storeapi

import (
	"github.com/docker/swarmkit/api"
	"github.com/docker/swarmkit/identity"
	"github.com/docker/swarmkit/manager/state/store"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
)

// CreateObject creates the specified object. The ID and Meta fields
// should be left empty, and will be filled in and included in the
// returned object.
func (s *Server) CreateObject(ctx context.Context, obj *api.Object) (*api.Object, error) {
	var filledObj *api.Object

	err := s.store.Update(func(tx store.Tx) error {
		switch v := obj.Object.(type) {
		case *api.Object_Node:
			if v.Node.ID != "" {
				return grpc.Errorf(codes.InvalidArgument, "object ID must be unset")
			}
			v.Node.ID = identity.NewID()
			// CreateNode fills in meta
			if err := store.CreateNode(tx, v.Node); err != nil {
				return err
			}
			filledObj = &api.Object{Object: &api.Object_Node{Node: v.Node}}
		case *api.Object_Service:
			if v.Service.ID != "" {
				return grpc.Errorf(codes.InvalidArgument, "object ID must be unset")
			}
			v.Service.ID = identity.NewID()
			// CreateService fills in meta
			if err := store.CreateService(tx, v.Service); err != nil {
				return err
			}
			filledObj = &api.Object{Object: &api.Object_Service{Service: v.Service}}
		case *api.Object_Network:
			if v.Network.ID != "" {
				return grpc.Errorf(codes.InvalidArgument, "object ID must be unset")
			}
			v.Network.ID = identity.NewID()
			// CreateNetwork fills in meta
			if err := store.CreateNetwork(tx, v.Network); err != nil {
				return err
			}
			filledObj = &api.Object{Object: &api.Object_Network{Network: v.Network}}
		case *api.Object_Task:
			if v.Task.ID != "" {
				return grpc.Errorf(codes.InvalidArgument, "object ID must be unset")
			}
			v.Task.ID = identity.NewID()
			// CreateTask fills in meta
			if err := store.CreateTask(tx, v.Task); err != nil {
				return err
			}
			filledObj = &api.Object{Object: &api.Object_Task{Task: v.Task}}
		case *api.Object_Cluster:
			if v.Cluster.ID != "" {
				return grpc.Errorf(codes.InvalidArgument, "object ID must be unset")
			}
			v.Cluster.ID = identity.NewID()
			// CreateCluster fills in meta
			if err := store.CreateCluster(tx, v.Cluster); err != nil {
				return err
			}
			filledObj = &api.Object{Object: &api.Object_Cluster{Cluster: v.Cluster}}
		case *api.Object_Secret:
			if v.Secret.ID != "" {
				return grpc.Errorf(codes.InvalidArgument, "object ID must be unset")
			}
			v.Secret.ID = identity.NewID()
			// CreateSecret fills in meta
			if err := store.CreateSecret(tx, v.Secret); err != nil {
				return err
			}
			filledObj = &api.Object{Object: &api.Object_Secret{Secret: v.Secret}}
		case *api.Object_Extension:
			if v.Extension.ID != "" {
				return grpc.Errorf(codes.InvalidArgument, "object ID must be unset")
			}
			v.Extension.ID = identity.NewID()
			// CreateExtension fills in meta
			if err := store.CreateExtension(tx, v.Extension); err != nil {
				return err
			}
			filledObj = &api.Object{Object: &api.Object_Extension{Extension: v.Extension}}
		case *api.Object_Resource:
			if v.Resource.ID != "" {
				return grpc.Errorf(codes.InvalidArgument, "object ID must be unset")
			}
			v.Resource.ID = identity.NewID()
			// CreateResource fills in meta
			if err := store.CreateResource(tx, v.Resource); err != nil {
				return err
			}
			filledObj = &api.Object{Object: &api.Object_Resource{Resource: v.Resource}}
		default:
			return grpc.Errorf(codes.InvalidArgument, "unrecognized object type %T", obj.Object)
		}
		return nil
	})

	return filledObj, err
}
