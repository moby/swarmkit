package storeapi

import (
	"github.com/docker/swarmkit/api"
	"github.com/docker/swarmkit/manager/state/store"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
)

func getBys(selectors []*api.SelectBy) (store.By, error) {
	var bys []store.By

	for _, selector := range selectors {
		switch v := selector.By.(type) {
		case *api.SelectBy_ID:
			return nil, grpc.Errorf(codes.InvalidArgument, "cannot search by ID - use GetObjects instead")
		case *api.SelectBy_IDPrefix:
			bys = append(bys, store.ByIDPrefix(v.IDPrefix))
		case *api.SelectBy_Name:
			bys = append(bys, store.ByName(v.Name))
		case *api.SelectBy_NamePrefix:
			bys = append(bys, store.ByNamePrefix(v.NamePrefix))
		case *api.SelectBy_ServiceID:
			bys = append(bys, store.ByServiceID(v.ServiceID))
		case *api.SelectBy_NodeID:
			bys = append(bys, store.ByNodeID(v.NodeID))
		case *api.SelectBy_Slot:
			bys = append(bys, store.BySlot(v.Slot.ServiceID, v.Slot.Slot))
		case *api.SelectBy_DesiredState:
			bys = append(bys, store.ByDesiredState(v.DesiredState))
		case *api.SelectBy_Role:
			bys = append(bys, store.ByRole(v.Role))
		case *api.SelectBy_Membership:
			bys = append(bys, store.ByMembership(v.Membership))
		case *api.SelectBy_ReferencedNetworkID:
			bys = append(bys, store.ByReferencedNetworkID(v.ReferencedNetworkID))
		case *api.SelectBy_ReferencedSecretID:
			bys = append(bys, store.ByReferencedSecretID(v.ReferencedSecretID))
		case *api.SelectBy_Kind:
			bys = append(bys, store.ByKind(v.Kind))
		case *api.SelectBy_Custom:
			bys = append(bys, store.ByCustom(v.Custom.Kind, v.Custom.Index, v.Custom.Value))
		case *api.SelectBy_CustomPrefix:
			bys = append(bys, store.ByCustomPrefix(v.CustomPrefix.Kind, v.CustomPrefix.Index, v.CustomPrefix.Value))
		default:
			return nil, grpc.Errorf(codes.InvalidArgument, "unrecognized selector type %T", selector.By)
		}
	}

	if len(bys) == 0 {
		return store.All, nil
	}
	if len(bys) == 1 {
		return bys[0], nil
	}
	return store.Or(bys...), nil
}

// FindObjects returns a set of objects of a given type, according to
// selectors included in the request.
func (s *Server) FindObjects(ctx context.Context, request *api.FindObjectsRequest) (*api.FindObjectsResponse, error) {
	if request.Kind == "" {
		return nil, grpc.Errorf(codes.InvalidArgument, "no kind of object specified")
	}

	// Convert selector list into appropriate arguments to Find*
	by, err := getBys(request.Selectors)
	if err != nil {
		return nil, err
	}

	var results []*api.Object

	s.store.View(func(readTx store.ReadTx) {
		switch request.Kind {
		case "node":
			var nodes []*api.Node
			nodes, err = store.FindNodes(readTx, by)
			if err == nil {
				for _, node := range nodes {
					results = append(results, &api.Object{Object: &api.Object_Node{Node: node}})
				}
			}
		case "service":
			var services []*api.Service
			services, err = store.FindServices(readTx, by)
			if err == nil {
				for _, service := range services {
					results = append(results, &api.Object{Object: &api.Object_Service{Service: service}})
				}
			}
		case "network":
			var networks []*api.Network
			networks, err = store.FindNetworks(readTx, by)
			if err == nil {
				for _, network := range networks {
					results = append(results, &api.Object{Object: &api.Object_Network{Network: network}})
				}
			}
		case "task":
			var tasks []*api.Task
			tasks, err = store.FindTasks(readTx, by)
			if err == nil {
				for _, task := range tasks {
					results = append(results, &api.Object{Object: &api.Object_Task{Task: task}})
				}
			}
		case "cluster":
			var clusters []*api.Cluster
			clusters, err = store.FindClusters(readTx, by)
			if err == nil {
				for _, cluster := range clusters {
					results = append(results, &api.Object{Object: &api.Object_Cluster{Cluster: cluster}})
				}
			}
		case "secret":
			var secrets []*api.Secret
			secrets, err = store.FindSecrets(readTx, by)
			if err == nil {
				for _, secret := range secrets {
					results = append(results, &api.Object{Object: &api.Object_Secret{Secret: secret}})
				}
			}
		case "extension":
			var extensions []*api.Extension
			extensions, err = store.FindExtensions(readTx, by)
			if err == nil {
				for _, extension := range extensions {
					results = append(results, &api.Object{Object: &api.Object_Extension{Extension: extension}})
				}
			}
		default:
			var resources []*api.Resource
			resources, err = store.FindResources(readTx, by)
			if err == nil {
				for _, resource := range resources {
					// FIXME(aaronl): This is inefficient.
					if resource.Kind == request.Kind {
						results = append(results, &api.Object{Object: &api.Object_Resource{Resource: resource}})
					}
				}
			}
		}
	})

	if err != nil {
		return nil, err
	}

	return &api.FindObjectsResponse{Results: results}, nil
}
