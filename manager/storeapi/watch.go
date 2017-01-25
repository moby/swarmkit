package storeapi

import (
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"

	"github.com/docker/swarmkit/api"
	"github.com/docker/swarmkit/manager/state"
)

var errConflictingFilters = grpc.Errorf(codes.InvalidArgument, "conflicting filters specified")

func convertNodeWatch(action api.StoreActionKind, filters []*api.SelectBy) (api.Event, error) {
	var (
		node          api.Node
		checkFuncs    []api.NodeCheckFunc
		hasRole       bool
		hasMembership bool
	)

	for _, filter := range filters {
		switch v := filter.By.(type) {
		case *api.SelectBy_ID:
			if node.ID != "" {
				return nil, errConflictingFilters
			}
			node.ID = v.ID
			checkFuncs = append(checkFuncs, state.NodeCheckID)
		case *api.SelectBy_IDPrefix:
			if node.ID != "" {
				return nil, errConflictingFilters
			}
			node.ID = v.IDPrefix
			checkFuncs = append(checkFuncs, state.NodeCheckIDPrefix)
		case *api.SelectBy_Name:
			if node.Description != nil {
				return nil, errConflictingFilters
			}
			node.Description = &api.NodeDescription{Hostname: v.Name}
			checkFuncs = append(checkFuncs, state.NodeCheckName)
		case *api.SelectBy_NamePrefix:
			if node.Description != nil {
				return nil, errConflictingFilters
			}
			node.Description = &api.NodeDescription{Hostname: v.NamePrefix}
			checkFuncs = append(checkFuncs, state.NodeCheckNamePrefix)
		case *api.SelectBy_Custom:
			// TODO(aaronl): Support multiple custom indices
			if len(node.Spec.Annotations.Indices) != 0 {
				return nil, errConflictingFilters
			}
			node.Spec.Annotations.Indices = []api.IndexEntry{{Key: v.Custom.Index, Val: v.Custom.Value}}
			checkFuncs = append(checkFuncs, state.NodeCheckCustom)
		case *api.SelectBy_CustomPrefix:
			// TODO(aaronl): Support multiple custom indices
			if len(node.Spec.Annotations.Indices) != 0 {
				return nil, errConflictingFilters
			}
			node.Spec.Annotations.Indices = []api.IndexEntry{{Key: v.CustomPrefix.Index, Val: v.CustomPrefix.Value}}
			checkFuncs = append(checkFuncs, state.NodeCheckCustomPrefix)
		case *api.SelectBy_Role:
			if hasRole {
				return nil, errConflictingFilters
			}
			node.Role = v.Role
			checkFuncs = append(checkFuncs, state.NodeCheckRole)
			hasRole = true
		case *api.SelectBy_Membership:
			if hasMembership {
				return nil, errConflictingFilters
			}
			node.Spec.Membership = v.Membership
			checkFuncs = append(checkFuncs, state.NodeCheckMembership)
			hasMembership = true
		default:
			return nil, grpc.Errorf(codes.InvalidArgument, "selector type %T is unsupported for nodes", filter.By)
		}
	}

	switch action {
	case api.StoreActionKindCreate:
		return api.EventCreateNode{Node: &node, Checks: checkFuncs}, nil
	case api.StoreActionKindUpdate:
		return api.EventUpdateNode{Node: &node, Checks: checkFuncs}, nil
	case api.StoreActionKindRemove:
		return api.EventDeleteNode{Node: &node, Checks: checkFuncs}, nil
	default:
		return nil, grpc.Errorf(codes.InvalidArgument, "unrecognized store action %v", action)
	}
}

func convertServiceWatch(action api.StoreActionKind, filters []*api.SelectBy) (api.Event, error) {
	var (
		service    api.Service
		checkFuncs []api.ServiceCheckFunc
	)

	for _, filter := range filters {
		switch v := filter.By.(type) {
		case *api.SelectBy_ID:
			if service.ID != "" {
				return nil, errConflictingFilters
			}
			service.ID = v.ID
			checkFuncs = append(checkFuncs, state.ServiceCheckID)
		case *api.SelectBy_IDPrefix:
			if service.ID != "" {
				return nil, errConflictingFilters
			}
			service.ID = v.IDPrefix
			checkFuncs = append(checkFuncs, state.ServiceCheckIDPrefix)
		case *api.SelectBy_Name:
			if service.Spec.Annotations.Name != "" {
				return nil, errConflictingFilters
			}
			service.Spec.Annotations.Name = v.Name
			checkFuncs = append(checkFuncs, state.ServiceCheckName)
		case *api.SelectBy_NamePrefix:
			if service.Spec.Annotations.Name != "" {
				return nil, errConflictingFilters
			}
			service.Spec.Annotations.Name = v.NamePrefix
			checkFuncs = append(checkFuncs, state.ServiceCheckNamePrefix)
		case *api.SelectBy_Custom:
			// TODO(aaronl): Support multiple custom indices
			if len(service.Spec.Annotations.Indices) != 0 {
				return nil, errConflictingFilters
			}
			service.Spec.Annotations.Indices = []api.IndexEntry{{Key: v.Custom.Index, Val: v.Custom.Value}}
			checkFuncs = append(checkFuncs, state.ServiceCheckCustom)
		case *api.SelectBy_CustomPrefix:
			// TODO(aaronl): Support multiple custom indices
			if len(service.Spec.Annotations.Indices) != 0 {
				return nil, errConflictingFilters
			}
			service.Spec.Annotations.Indices = []api.IndexEntry{{Key: v.CustomPrefix.Index, Val: v.CustomPrefix.Value}}
			checkFuncs = append(checkFuncs, state.ServiceCheckCustomPrefix)
		case *api.SelectBy_ReferencedNetworkID:
			// TODO(aaronl): not supported for now
			return nil, grpc.Errorf(codes.InvalidArgument, "selector type %T is unsupported for services", filter.By)
		case *api.SelectBy_ReferencedSecretID:
			// TODO(aaronl): not supported for now
			return nil, grpc.Errorf(codes.InvalidArgument, "selector type %T is unsupported for services", filter.By)
		default:
			return nil, grpc.Errorf(codes.InvalidArgument, "selector type %T is unsupported for services", filter.By)
		}
	}

	switch action {
	case api.StoreActionKindCreate:
		return api.EventCreateService{Service: &service, Checks: checkFuncs}, nil
	case api.StoreActionKindUpdate:
		return api.EventUpdateService{Service: &service, Checks: checkFuncs}, nil
	case api.StoreActionKindRemove:
		return api.EventDeleteService{Service: &service, Checks: checkFuncs}, nil
	default:
		return nil, grpc.Errorf(codes.InvalidArgument, "unrecognized store action %v", action)
	}
}

func convertNetworkWatch(action api.StoreActionKind, filters []*api.SelectBy) (api.Event, error) {
	var (
		network    api.Network
		checkFuncs []api.NetworkCheckFunc
	)

	for _, filter := range filters {
		switch v := filter.By.(type) {
		case *api.SelectBy_ID:
			if network.ID != "" {
				return nil, errConflictingFilters
			}
			network.ID = v.ID
			checkFuncs = append(checkFuncs, state.NetworkCheckID)
		case *api.SelectBy_IDPrefix:
			if network.ID != "" {
				return nil, errConflictingFilters
			}
			network.ID = v.IDPrefix
			checkFuncs = append(checkFuncs, state.NetworkCheckIDPrefix)
		case *api.SelectBy_Name:
			if network.Spec.Annotations.Name != "" {
				return nil, errConflictingFilters
			}
			network.Spec.Annotations.Name = v.Name
			checkFuncs = append(checkFuncs, state.NetworkCheckName)
		case *api.SelectBy_NamePrefix:
			if network.Spec.Annotations.Name != "" {
				return nil, errConflictingFilters
			}
			network.Spec.Annotations.Name = v.NamePrefix
			checkFuncs = append(checkFuncs, state.NetworkCheckNamePrefix)
		case *api.SelectBy_Custom:
			// TODO(aaronl): Support multiple custom indices
			if len(network.Spec.Annotations.Indices) != 0 {
				return nil, errConflictingFilters
			}
			network.Spec.Annotations.Indices = []api.IndexEntry{{Key: v.Custom.Index, Val: v.Custom.Value}}
			checkFuncs = append(checkFuncs, state.NetworkCheckCustom)
		case *api.SelectBy_CustomPrefix:
			// TODO(aaronl): Support multiple custom indices
			if len(network.Spec.Annotations.Indices) != 0 {
				return nil, errConflictingFilters
			}
			network.Spec.Annotations.Indices = []api.IndexEntry{{Key: v.CustomPrefix.Index, Val: v.CustomPrefix.Value}}
			checkFuncs = append(checkFuncs, state.NetworkCheckCustomPrefix)
		default:
			return nil, grpc.Errorf(codes.InvalidArgument, "selector type %T is unsupported for networks", filter.By)
		}
	}

	switch action {
	case api.StoreActionKindCreate:
		return api.EventCreateNetwork{Network: &network, Checks: checkFuncs}, nil
	case api.StoreActionKindUpdate:
		return api.EventUpdateNetwork{Network: &network, Checks: checkFuncs}, nil
	case api.StoreActionKindRemove:
		return api.EventDeleteNetwork{Network: &network, Checks: checkFuncs}, nil
	default:
		return nil, grpc.Errorf(codes.InvalidArgument, "unrecognized store action %v", action)
	}
}

func convertClusterWatch(action api.StoreActionKind, filters []*api.SelectBy) (api.Event, error) {
	var (
		cluster    api.Cluster
		checkFuncs []api.ClusterCheckFunc
	)

	for _, filter := range filters {
		switch v := filter.By.(type) {
		case *api.SelectBy_ID:
			if cluster.ID != "" {
				return nil, errConflictingFilters
			}
			cluster.ID = v.ID
			checkFuncs = append(checkFuncs, state.ClusterCheckID)
		case *api.SelectBy_IDPrefix:
			if cluster.ID != "" {
				return nil, errConflictingFilters
			}
			cluster.ID = v.IDPrefix
			checkFuncs = append(checkFuncs, state.ClusterCheckIDPrefix)
		case *api.SelectBy_Name:
			if cluster.Spec.Annotations.Name != "" {
				return nil, errConflictingFilters
			}
			cluster.Spec.Annotations.Name = v.Name
			checkFuncs = append(checkFuncs, state.ClusterCheckName)
		case *api.SelectBy_NamePrefix:
			if cluster.Spec.Annotations.Name != "" {
				return nil, errConflictingFilters
			}
			cluster.Spec.Annotations.Name = v.NamePrefix
			checkFuncs = append(checkFuncs, state.ClusterCheckNamePrefix)
		case *api.SelectBy_Custom:
			// TODO(aaronl): Support multiple custom indices
			if len(cluster.Spec.Annotations.Indices) != 0 {
				return nil, errConflictingFilters
			}
			cluster.Spec.Annotations.Indices = []api.IndexEntry{{Key: v.Custom.Index, Val: v.Custom.Value}}
			checkFuncs = append(checkFuncs, state.ClusterCheckCustom)
		case *api.SelectBy_CustomPrefix:
			// TODO(aaronl): Support multiple custom indices
			if len(cluster.Spec.Annotations.Indices) != 0 {
				return nil, errConflictingFilters
			}
			cluster.Spec.Annotations.Indices = []api.IndexEntry{{Key: v.CustomPrefix.Index, Val: v.CustomPrefix.Value}}
			checkFuncs = append(checkFuncs, state.ClusterCheckCustomPrefix)
		default:
			return nil, grpc.Errorf(codes.InvalidArgument, "selector type %T is unsupported for clusters", filter.By)
		}
	}

	switch action {
	case api.StoreActionKindCreate:
		return api.EventCreateCluster{Cluster: &cluster, Checks: checkFuncs}, nil
	case api.StoreActionKindUpdate:
		return api.EventUpdateCluster{Cluster: &cluster, Checks: checkFuncs}, nil
	case api.StoreActionKindRemove:
		return api.EventDeleteCluster{Cluster: &cluster, Checks: checkFuncs}, nil
	default:
		return nil, grpc.Errorf(codes.InvalidArgument, "unrecognized store action %v", action)
	}
}

func convertSecretWatch(action api.StoreActionKind, filters []*api.SelectBy) (api.Event, error) {
	var (
		secret     api.Secret
		checkFuncs []api.SecretCheckFunc
	)

	for _, filter := range filters {
		switch v := filter.By.(type) {
		case *api.SelectBy_ID:
			if secret.ID != "" {
				return nil, errConflictingFilters
			}
			secret.ID = v.ID
			checkFuncs = append(checkFuncs, state.SecretCheckID)
		case *api.SelectBy_IDPrefix:
			if secret.ID != "" {
				return nil, errConflictingFilters
			}
			secret.ID = v.IDPrefix
			checkFuncs = append(checkFuncs, state.SecretCheckIDPrefix)
		case *api.SelectBy_Name:
			if secret.Spec.Annotations.Name != "" {
				return nil, errConflictingFilters
			}
			secret.Spec.Annotations.Name = v.Name
			checkFuncs = append(checkFuncs, state.SecretCheckName)
		case *api.SelectBy_NamePrefix:
			if secret.Spec.Annotations.Name != "" {
				return nil, errConflictingFilters
			}
			secret.Spec.Annotations.Name = v.NamePrefix
			checkFuncs = append(checkFuncs, state.SecretCheckNamePrefix)
		case *api.SelectBy_Custom:
			// TODO(aaronl): Support multiple custom indices
			if len(secret.Spec.Annotations.Indices) != 0 {
				return nil, errConflictingFilters
			}
			secret.Spec.Annotations.Indices = []api.IndexEntry{{Key: v.Custom.Index, Val: v.Custom.Value}}
			checkFuncs = append(checkFuncs, state.SecretCheckCustom)
		case *api.SelectBy_CustomPrefix:
			// TODO(aaronl): Support multiple custom indices
			if len(secret.Spec.Annotations.Indices) != 0 {
				return nil, errConflictingFilters
			}
			secret.Spec.Annotations.Indices = []api.IndexEntry{{Key: v.CustomPrefix.Index, Val: v.CustomPrefix.Value}}
			checkFuncs = append(checkFuncs, state.SecretCheckCustomPrefix)
		default:
			return nil, grpc.Errorf(codes.InvalidArgument, "selector type %T is unsupported for secrets", filter.By)
		}
	}

	switch action {
	case api.StoreActionKindCreate:
		return api.EventCreateSecret{Secret: &secret, Checks: checkFuncs}, nil
	case api.StoreActionKindUpdate:
		return api.EventUpdateSecret{Secret: &secret, Checks: checkFuncs}, nil
	case api.StoreActionKindRemove:
		return api.EventDeleteSecret{Secret: &secret, Checks: checkFuncs}, nil
	default:
		return nil, grpc.Errorf(codes.InvalidArgument, "unrecognized store action %v", action)
	}
}

func convertTaskWatch(action api.StoreActionKind, filters []*api.SelectBy) (api.Event, error) {
	var (
		task            api.Task
		checkFuncs      []api.TaskCheckFunc
		hasDesiredState bool
	)

	for _, filter := range filters {
		switch v := filter.By.(type) {
		case *api.SelectBy_ID:
			if task.ID != "" {
				return nil, errConflictingFilters
			}
			task.ID = v.ID
			checkFuncs = append(checkFuncs, state.TaskCheckID)
		case *api.SelectBy_IDPrefix:
			if task.ID != "" {
				return nil, errConflictingFilters
			}
			task.ID = v.IDPrefix
			checkFuncs = append(checkFuncs, state.TaskCheckIDPrefix)
		case *api.SelectBy_Custom:
			// TODO(aaronl): Support multiple custom indices
			if len(task.Annotations.Indices) != 0 {
				return nil, errConflictingFilters
			}
			task.Annotations.Indices = []api.IndexEntry{{Key: v.Custom.Index, Val: v.Custom.Value}}
			checkFuncs = append(checkFuncs, state.TaskCheckCustom)
		case *api.SelectBy_CustomPrefix:
			// TODO(aaronl): Support multiple custom indices
			if len(task.Annotations.Indices) != 0 {
				return nil, errConflictingFilters
			}
			task.Annotations.Indices = []api.IndexEntry{{Key: v.CustomPrefix.Index, Val: v.CustomPrefix.Value}}
			checkFuncs = append(checkFuncs, state.TaskCheckCustomPrefix)
		case *api.SelectBy_ServiceID:
			if task.ServiceID != "" {
				return nil, errConflictingFilters
			}
			task.ServiceID = v.ServiceID
			checkFuncs = append(checkFuncs, state.TaskCheckServiceID)
		case *api.SelectBy_NodeID:
			if task.NodeID != "" {
				return nil, errConflictingFilters
			}
			task.NodeID = v.NodeID
			checkFuncs = append(checkFuncs, state.TaskCheckNodeID)
		case *api.SelectBy_Slot:
			if task.Slot != 0 || task.ServiceID != "" {
				return nil, errConflictingFilters
			}
			task.ServiceID = v.Slot.ServiceID
			task.Slot = v.Slot.Slot
			checkFuncs = append(checkFuncs, state.TaskCheckSlot, state.TaskCheckServiceID)
		case *api.SelectBy_DesiredState:
			if hasDesiredState {
				return nil, errConflictingFilters
			}
			task.DesiredState = v.DesiredState
			checkFuncs = append(checkFuncs, state.TaskCheckDesiredState)
			hasDesiredState = true
		default:
			return nil, grpc.Errorf(codes.InvalidArgument, "selector type %T is unsupported for tasks", filter.By)
		}
	}

	switch action {
	case api.StoreActionKindCreate:
		return api.EventCreateTask{Task: &task, Checks: checkFuncs}, nil
	case api.StoreActionKindUpdate:
		return api.EventUpdateTask{Task: &task, Checks: checkFuncs}, nil
	case api.StoreActionKindRemove:
		return api.EventDeleteTask{Task: &task, Checks: checkFuncs}, nil
	default:
		return nil, grpc.Errorf(codes.InvalidArgument, "unrecognized store action %v", action)
	}
}

func convertExtensionWatch(action api.StoreActionKind, filters []*api.SelectBy) (api.Event, error) {
	var (
		extension  api.Extension
		checkFuncs []api.ExtensionCheckFunc
	)

	for _, filter := range filters {
		switch v := filter.By.(type) {
		case *api.SelectBy_ID:
			if extension.ID != "" {
				return nil, errConflictingFilters
			}
			extension.ID = v.ID
			checkFuncs = append(checkFuncs, state.ExtensionCheckID)
		case *api.SelectBy_IDPrefix:
			if extension.ID != "" {
				return nil, errConflictingFilters
			}
			extension.ID = v.IDPrefix
			checkFuncs = append(checkFuncs, state.ExtensionCheckIDPrefix)
		case *api.SelectBy_Name:
			if extension.Annotations.Name != "" {
				return nil, errConflictingFilters
			}
			extension.Annotations.Name = v.Name
			checkFuncs = append(checkFuncs, state.ExtensionCheckName)
		case *api.SelectBy_NamePrefix:
			if extension.Annotations.Name != "" {
				return nil, errConflictingFilters
			}
			extension.Annotations.Name = v.NamePrefix
			checkFuncs = append(checkFuncs, state.ExtensionCheckNamePrefix)
		case *api.SelectBy_Custom:
			// TODO(aaronl): Support multiple custom indices
			if len(extension.Annotations.Indices) != 0 {
				return nil, errConflictingFilters
			}
			extension.Annotations.Indices = []api.IndexEntry{{Key: v.Custom.Index, Val: v.Custom.Value}}
			checkFuncs = append(checkFuncs, state.ExtensionCheckCustom)
		case *api.SelectBy_CustomPrefix:
			// TODO(aaronl): Support multiple custom indices
			if len(extension.Annotations.Indices) != 0 {
				return nil, errConflictingFilters
			}
			extension.Annotations.Indices = []api.IndexEntry{{Key: v.CustomPrefix.Index, Val: v.CustomPrefix.Value}}
			checkFuncs = append(checkFuncs, state.ExtensionCheckCustomPrefix)
		default:
			return nil, grpc.Errorf(codes.InvalidArgument, "selector type %T is unsupported for extensions", filter.By)
		}
	}

	switch action {
	case api.StoreActionKindCreate:
		return api.EventCreateExtension{Extension: &extension, Checks: checkFuncs}, nil
	case api.StoreActionKindUpdate:
		return api.EventUpdateExtension{Extension: &extension, Checks: checkFuncs}, nil
	case api.StoreActionKindRemove:
		return api.EventDeleteExtension{Extension: &extension, Checks: checkFuncs}, nil
	default:
		return nil, grpc.Errorf(codes.InvalidArgument, "unrecognized store action %v", action)
	}
}

func convertResourceWatch(action api.StoreActionKind, filters []*api.SelectBy, kind string) (api.Event, error) {
	resource := api.Resource{Kind: kind}
	checkFuncs := []api.ResourceCheckFunc{state.ResourceCheckKind}

	for _, filter := range filters {
		switch v := filter.By.(type) {
		case *api.SelectBy_ID:
			if resource.ID != "" {
				return nil, errConflictingFilters
			}
			resource.ID = v.ID
			checkFuncs = append(checkFuncs, state.ResourceCheckID)
		case *api.SelectBy_IDPrefix:
			if resource.ID != "" {
				return nil, errConflictingFilters
			}
			resource.ID = v.IDPrefix
			checkFuncs = append(checkFuncs, state.ResourceCheckIDPrefix)
		case *api.SelectBy_Name:
			if resource.Annotations.Name != "" {
				return nil, errConflictingFilters
			}
			resource.Annotations.Name = v.Name
			checkFuncs = append(checkFuncs, state.ResourceCheckName)
		case *api.SelectBy_NamePrefix:
			if resource.Annotations.Name != "" {
				return nil, errConflictingFilters
			}
			resource.Annotations.Name = v.NamePrefix
			checkFuncs = append(checkFuncs, state.ResourceCheckNamePrefix)
		case *api.SelectBy_Custom:
			// TODO(aaronl): Support multiple custom indices
			if len(resource.Annotations.Indices) != 0 {
				return nil, errConflictingFilters
			}
			resource.Annotations.Indices = []api.IndexEntry{{Key: v.Custom.Index, Val: v.Custom.Value}}
			checkFuncs = append(checkFuncs, state.ResourceCheckCustom)
		case *api.SelectBy_CustomPrefix:
			// TODO(aaronl): Support multiple custom indices
			if len(resource.Annotations.Indices) != 0 {
				return nil, errConflictingFilters
			}
			resource.Annotations.Indices = []api.IndexEntry{{Key: v.CustomPrefix.Index, Val: v.CustomPrefix.Value}}
			checkFuncs = append(checkFuncs, state.ResourceCheckCustomPrefix)
		default:
			return nil, grpc.Errorf(codes.InvalidArgument, "selector type %T is unsupported for resource objects", filter.By)
		}
	}

	switch action {
	case api.StoreActionKindCreate:
		return api.EventCreateResource{Resource: &resource, Checks: checkFuncs}, nil
	case api.StoreActionKindUpdate:
		return api.EventUpdateResource{Resource: &resource, Checks: checkFuncs}, nil
	case api.StoreActionKindRemove:
		return api.EventDeleteResource{Resource: &resource, Checks: checkFuncs}, nil
	default:
		return nil, grpc.Errorf(codes.InvalidArgument, "unrecognized store action %v", action)
	}
}

func convertWatchArgs(entries []*api.WatchRequest_WatchEntry) ([]api.Event, error) {
	var events []api.Event

	for _, entry := range entries {
		var (
			event api.Event
			err   error
		)
		switch entry.Kind {
		case "":
			return nil, grpc.Errorf(codes.InvalidArgument, "no kind of object specified")
		case "node":
			event, err = convertNodeWatch(entry.Action, entry.Filters)
		case "service":
			event, err = convertServiceWatch(entry.Action, entry.Filters)
		case "network":
			event, err = convertNetworkWatch(entry.Action, entry.Filters)
		case "task":
			event, err = convertTaskWatch(entry.Action, entry.Filters)
		case "cluster":
			event, err = convertClusterWatch(entry.Action, entry.Filters)
		case "secret":
			event, err = convertSecretWatch(entry.Action, entry.Filters)
		case "extension":
			event, err = convertExtensionWatch(entry.Action, entry.Filters)
		default:
			event, err = convertResourceWatch(entry.Action, entry.Filters, entry.Kind)
		}
		if err != nil {
			return nil, err
		}
		events = append(events, event)
	}

	return events, nil
}

func watchMessage(event api.Event) *api.WatchMessage {
	switch v := event.(type) {
	case api.EventCreateTask:
		return &api.WatchMessage{Action: api.StoreActionKindCreate, Object: &api.Object{Object: &api.Object_Task{Task: v.Task}}}
	case api.EventUpdateTask:
		return &api.WatchMessage{Action: api.StoreActionKindUpdate, Object: &api.Object{Object: &api.Object_Task{Task: v.Task}}}
	case api.EventDeleteTask:
		return &api.WatchMessage{Action: api.StoreActionKindRemove, Object: &api.Object{Object: &api.Object_Task{Task: v.Task}}}
	case api.EventCreateService:
		return &api.WatchMessage{Action: api.StoreActionKindCreate, Object: &api.Object{Object: &api.Object_Service{Service: v.Service}}}
	case api.EventUpdateService:
		return &api.WatchMessage{Action: api.StoreActionKindUpdate, Object: &api.Object{Object: &api.Object_Service{Service: v.Service}}}
	case api.EventDeleteService:
		return &api.WatchMessage{Action: api.StoreActionKindRemove, Object: &api.Object{Object: &api.Object_Service{Service: v.Service}}}
	case api.EventCreateNetwork:
		return &api.WatchMessage{Action: api.StoreActionKindCreate, Object: &api.Object{Object: &api.Object_Network{Network: v.Network}}}
	case api.EventUpdateNetwork:
		return &api.WatchMessage{Action: api.StoreActionKindUpdate, Object: &api.Object{Object: &api.Object_Network{Network: v.Network}}}
	case api.EventDeleteNetwork:
		return &api.WatchMessage{Action: api.StoreActionKindRemove, Object: &api.Object{Object: &api.Object_Network{Network: v.Network}}}
	case api.EventCreateNode:
		return &api.WatchMessage{Action: api.StoreActionKindCreate, Object: &api.Object{Object: &api.Object_Node{Node: v.Node}}}
	case api.EventUpdateNode:
		return &api.WatchMessage{Action: api.StoreActionKindUpdate, Object: &api.Object{Object: &api.Object_Node{Node: v.Node}}}
	case api.EventDeleteNode:
		return &api.WatchMessage{Action: api.StoreActionKindRemove, Object: &api.Object{Object: &api.Object_Node{Node: v.Node}}}
	case api.EventCreateCluster:
		return &api.WatchMessage{Action: api.StoreActionKindCreate, Object: &api.Object{Object: &api.Object_Cluster{Cluster: v.Cluster}}}
	case api.EventUpdateCluster:
		return &api.WatchMessage{Action: api.StoreActionKindUpdate, Object: &api.Object{Object: &api.Object_Cluster{Cluster: v.Cluster}}}
	case api.EventDeleteCluster:
		return &api.WatchMessage{Action: api.StoreActionKindRemove, Object: &api.Object{Object: &api.Object_Cluster{Cluster: v.Cluster}}}
	case api.EventCreateSecret:
		return &api.WatchMessage{Action: api.StoreActionKindCreate, Object: &api.Object{Object: &api.Object_Secret{Secret: v.Secret}}}
	case api.EventUpdateSecret:
		return &api.WatchMessage{Action: api.StoreActionKindUpdate, Object: &api.Object{Object: &api.Object_Secret{Secret: v.Secret}}}
	case api.EventDeleteSecret:
		return &api.WatchMessage{Action: api.StoreActionKindRemove, Object: &api.Object{Object: &api.Object_Secret{Secret: v.Secret}}}
	case api.EventCreateResource:
		return &api.WatchMessage{Action: api.StoreActionKindCreate, Object: &api.Object{Object: &api.Object_Resource{Resource: v.Resource}}}
	case api.EventUpdateResource:
		return &api.WatchMessage{Action: api.StoreActionKindUpdate, Object: &api.Object{Object: &api.Object_Resource{Resource: v.Resource}}}
	case api.EventDeleteResource:
		return &api.WatchMessage{Action: api.StoreActionKindRemove, Object: &api.Object{Object: &api.Object_Resource{Resource: v.Resource}}}
	case api.EventCreateExtension:
		return &api.WatchMessage{Action: api.StoreActionKindCreate, Object: &api.Object{Object: &api.Object_Extension{Extension: v.Extension}}}
	case api.EventUpdateExtension:
		return &api.WatchMessage{Action: api.StoreActionKindUpdate, Object: &api.Object{Object: &api.Object_Extension{Extension: v.Extension}}}
	case api.EventDeleteExtension:
		return &api.WatchMessage{Action: api.StoreActionKindRemove, Object: &api.Object{Object: &api.Object_Extension{Extension: v.Extension}}}
	}
	return nil
}

// Watch starts a stream that returns any changes to objects that match
// the specified selectors. When the stream begins, it immediately sends
// an empty message back to the client. It is important to wait for
// this message before taking any actions that depend on an established
// stream of changes for consistency.
func (s *Server) Watch(request *api.WatchRequest, stream api.Store_WatchServer) error {
	ctx := stream.Context()

	watchArgs, err := convertWatchArgs(request.Entries)
	if err != nil {
		return err
	}

	watch, cancel := state.Watch(s.store.WatchQueue(), watchArgs...)
	defer cancel()

	if err := stream.Send(&api.WatchMessage{}); err != nil {
		return err
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case event := <-watch:
			message := watchMessage(event.(api.Event))
			if message != nil {
				if err := stream.Send(message); err != nil {
					return err
				}
			}
		}
	}
}
