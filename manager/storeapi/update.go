package storeapi

import (
	"github.com/docker/swarmkit/api"
	"github.com/docker/swarmkit/manager/state/store"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
)

// UpdateObjects atomically commits the supplied objects to the data store.
// - Returns `FailedPrecondition` and does not commit any changes if
//   any of the objects have changed since they were retrieved.
func (s *Server) UpdateObjects(ctx context.Context, request *api.UpdateObjectsRequest) (*api.UpdateObjectsResponse, error) {
	var updatedObjs []*api.Object

	err := s.store.Update(func(tx store.Tx) error {
		for _, obj := range request.Objects {
			switch v := obj.Object.(type) {
			case *api.Object_Node:
				// UpdateNode fills in meta
				if err := store.UpdateNode(tx, v.Node); err != nil {
					return err
				}
				updatedObjs = append(updatedObjs, &api.Object{Object: &api.Object_Node{Node: v.Node}})
			case *api.Object_Service:
				// UpdateService fills in meta
				if err := store.UpdateService(tx, v.Service); err != nil {
					return err
				}
				updatedObjs = append(updatedObjs, &api.Object{Object: &api.Object_Service{Service: v.Service}})
			case *api.Object_Network:
				// UpdateNetwork fills in meta
				if err := store.UpdateNetwork(tx, v.Network); err != nil {
					return err
				}
				updatedObjs = append(updatedObjs, &api.Object{Object: &api.Object_Network{Network: v.Network}})
			case *api.Object_Task:
				// UpdateTask fills in meta
				if err := store.UpdateTask(tx, v.Task); err != nil {
					return err
				}
				updatedObjs = append(updatedObjs, &api.Object{Object: &api.Object_Task{Task: v.Task}})
			case *api.Object_Cluster:
				// UpdateCluster fills in meta
				if err := store.UpdateCluster(tx, v.Cluster); err != nil {
					return err
				}
				updatedObjs = append(updatedObjs, &api.Object{Object: &api.Object_Cluster{Cluster: v.Cluster}})
			case *api.Object_Secret:
				// UpdateSecret fills in meta
				if err := store.UpdateSecret(tx, v.Secret); err != nil {
					return err
				}
				updatedObjs = append(updatedObjs, &api.Object{Object: &api.Object_Secret{Secret: v.Secret}})
			case *api.Object_Extension:
				// UpdateExtension fills in meta
				if err := store.UpdateExtension(tx, v.Extension); err != nil {
					return err
				}
				updatedObjs = append(updatedObjs, &api.Object{Object: &api.Object_Extension{Extension: v.Extension}})
			case *api.Object_Resource:
				// UpdateResource fills in meta
				if err := store.UpdateResource(tx, v.Resource); err != nil {
					return err
				}
				updatedObjs = append(updatedObjs, &api.Object{Object: &api.Object_Resource{Resource: v.Resource}})
			default:
				return grpc.Errorf(codes.InvalidArgument, "unrecognized object type")
			}
		}
		return nil
	})

	if err != nil {
		return nil, err
	}

	return &api.UpdateObjectsResponse{Objects: updatedObjs}, nil
}
