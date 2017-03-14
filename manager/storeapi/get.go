package storeapi

import (
	"github.com/docker/swarmkit/api"
	"github.com/docker/swarmkit/manager/state/store"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
)

// GetObject retrieves an object by its ID.
// - Returns `NotFound` if the object is not found.
func (s *Server) GetObject(ctx context.Context, request *api.GetObjectRequest) (*api.Object, error) {
	if request.ObjectID == "" {
		return nil, grpc.Errorf(codes.InvalidArgument, "no object ID specified")
	}
	if request.Kind == "" {
		return nil, grpc.Errorf(codes.InvalidArgument, "no kind of object specified")
	}

	var obj *api.Object

	s.store.View(func(readTx store.ReadTx) {
		switch request.Kind {
		case "node":
			node := store.GetNode(readTx, request.ObjectID)
			if node != nil {
				obj = &api.Object{Object: &api.Object_Node{Node: node}}
			}
		case "service":
			service := store.GetService(readTx, request.ObjectID)
			if service != nil {
				obj = &api.Object{Object: &api.Object_Service{Service: service}}
			}
		case "network":
			network := store.GetNetwork(readTx, request.ObjectID)
			if network != nil {
				obj = &api.Object{Object: &api.Object_Network{Network: network}}
			}
		case "task":
			task := store.GetTask(readTx, request.ObjectID)
			if task != nil {
				obj = &api.Object{Object: &api.Object_Task{Task: task}}
			}
		case "cluster":
			cluster := store.GetCluster(readTx, request.ObjectID)
			if cluster != nil {
				obj = &api.Object{Object: &api.Object_Cluster{Cluster: cluster}}
			}
		case "secret":
			secret := store.GetSecret(readTx, request.ObjectID)
			if secret != nil {
				obj = &api.Object{Object: &api.Object_Secret{Secret: secret}}
			}
		case "extension":
			extension := store.GetExtension(readTx, request.ObjectID)
			if extension != nil {
				obj = &api.Object{Object: &api.Object_Extension{Extension: extension}}
			}
		default:
			resource := store.GetResource(readTx, request.ObjectID)
			if resource != nil {
				obj = &api.Object{Object: &api.Object_Resource{Resource: resource}}
			}
		}
	})

	if obj == nil {
		return nil, grpc.Errorf(codes.NotFound, "%s %s not found", request.Kind, request.ObjectID)
	}

	return obj, nil
}
