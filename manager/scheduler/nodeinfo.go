package scheduler

import (
	"context"
	"time"

	"github.com/docker/swarmkit/api"
	"github.com/docker/swarmkit/api/genericresource"
	"github.com/docker/swarmkit/log"
)

// hostPortSpec specifies a used host port.
type hostPortSpec struct {
	protocol      api.PortConfig_Protocol
	publishedPort uint32
}

// versionedService defines a tuple that contains a service ID and a spec
// version, so that failures can be tracked per spec version. Note that if the
// task predates spec versioning, specVersion will contain the zero value, and
// this will still work correctly.
type versionedService struct {
	serviceID   string
	specVersion api.Version
}

// NodeInfo contains a node and some additional metadata.
type NodeInfo struct {
	*api.Node
	Tasks                     map[string]*api.Task
	ActiveTasksCount          int
	ActiveTasksCountByService map[string]int
	AvailableResources        *api.Resources
	usedHostPorts             map[hostPortSpec]struct{}

	// recentFailures is a map from service ID/version to the timestamps of
	// the most recent failures the node has experienced from replicas of
	// that service.
	recentFailures map[versionedService][]time.Time

	// lastCleanup is the last time recentFailures was cleaned up. This is
	// done periodically to avoid recentFailures growing without any limit.
	lastCleanup time.Time

	// nodeDevices is a map of all devices on the node to the tasks that are
	// using those devices
	nodeDevices map[string]string

	// nodeDeviceClasses maps a DeviceClass ID to a set of Devices in that
	// class. this lets the filter check which devices belong to a class;
	// however, the filter will still need to check which devices are actually
	// free, as a device can belong to more than one class
	// this is a map of a map, instead of a map of a slice, to enforce a
	// randomization of device selection during iteration
	nodeDeviceClasses map[string]map[string]struct{}
}

func newNodeInfo(n *api.Node, tasks map[string]*api.Task, availableResources api.Resources) NodeInfo {
	nodeInfo := NodeInfo{
		Node:                      n,
		Tasks:                     make(map[string]*api.Task),
		ActiveTasksCountByService: make(map[string]int),
		AvailableResources:        availableResources.Copy(),
		usedHostPorts:             make(map[hostPortSpec]struct{}),
		recentFailures:            make(map[versionedService][]time.Time),
		lastCleanup:               time.Now(),
		nodeDevices:               make(map[string]string),
		nodeDeviceClasses:         make(map[string]map[string]struct{}),
	}

	// create the local nodeDevices tracking. we need to do this before we add
	// tasks so that adding tasks proceeds correctly.
	for _, dev := range n.Spec.Devices {
		if dev != nil {
			nodeInfo.nodeDevices[dev.Path] = ""
		}
		// check if the nodeDeviceClasses yet has a set for this deviceClass.
		// if not, create one now
		if _, ok := nodeInfo.nodeDeviceClasses[dev.DeviceClassID]; !ok {
			nodeInfo.nodeDeviceClasses[dev.DeviceClassID] = map[string]struct{}{}
		}
		// then, add this device to the deviceClasses set
		nodeInfo.nodeDeviceClasses[dev.DeviceClassID][dev.Path] = struct{}{}
	}

	for _, t := range tasks {
		nodeInfo.addTask(t)
	}

	return nodeInfo
}

// removeTask removes a task from nodeInfo if it's tracked there, and returns true
// if nodeInfo was modified.
func (nodeInfo *NodeInfo) removeTask(t *api.Task) bool {
	oldTask, ok := nodeInfo.Tasks[t.ID]
	if !ok {
		return false
	}

	delete(nodeInfo.Tasks, t.ID)
	if oldTask.DesiredState <= api.TaskStateRunning {
		nodeInfo.ActiveTasksCount--
		nodeInfo.ActiveTasksCountByService[t.ServiceID]--
	}

	if t.Endpoint != nil {
		for _, port := range t.Endpoint.Ports {
			if port.PublishMode == api.PublishModeHost && port.PublishedPort != 0 {
				portSpec := hostPortSpec{protocol: port.Protocol, publishedPort: port.PublishedPort}
				delete(nodeInfo.usedHostPorts, portSpec)
			}
		}
	}

	reservations := taskReservations(t.Spec)
	resources := nodeInfo.AvailableResources

	resources.MemoryBytes += reservations.MemoryBytes
	resources.NanoCPUs += reservations.NanoCPUs

	if nodeInfo.Description == nil || nodeInfo.Description.Resources == nil ||
		nodeInfo.Description.Resources.Generic == nil {
		return true
	}

	taskAssigned := t.AssignedGenericResources
	nodeAvailableResources := &resources.Generic
	nodeRes := nodeInfo.Description.Resources.Generic
	genericresource.Reclaim(nodeAvailableResources, taskAssigned, nodeRes)

	// additionally, release the device attachments.
	for dev, task := range nodeInfo.nodeDevices {
		if task == t.ID {
			// if a device is in use by this task, then set it to emptystring,
			// because it's no longer in use
			nodeInfo.nodeDevices[dev] = ""
		}
	}

	return true
}

// addTask adds or updates a task on nodeInfo, and returns true if nodeInfo was
// modified. the task may be modified.
func (nodeInfo *NodeInfo) addTask(t *api.Task) bool {
	oldTask, ok := nodeInfo.Tasks[t.ID]
	if ok {
		if t.DesiredState <= api.TaskStateRunning && oldTask.DesiredState > api.TaskStateRunning {
			nodeInfo.Tasks[t.ID] = t
			nodeInfo.ActiveTasksCount++
			nodeInfo.ActiveTasksCountByService[t.ServiceID]++
			return true
		} else if t.DesiredState > api.TaskStateRunning && oldTask.DesiredState <= api.TaskStateRunning {
			nodeInfo.Tasks[t.ID] = t
			nodeInfo.ActiveTasksCount--
			nodeInfo.ActiveTasksCountByService[t.ServiceID]--
			return true
		}
		return false
	}

	nodeInfo.Tasks[t.ID] = t

	reservations := taskReservations(t.Spec)
	resources := nodeInfo.AvailableResources

	resources.MemoryBytes -= reservations.MemoryBytes
	resources.NanoCPUs -= reservations.NanoCPUs

	// minimum size required
	t.AssignedGenericResources = make([]*api.GenericResource, 0, len(resources.Generic))
	taskAssigned := &t.AssignedGenericResources

	genericresource.Claim(&resources.Generic, taskAssigned, reservations.Generic)

	if t.Endpoint != nil {
		for _, port := range t.Endpoint.Ports {
			if port.PublishMode == api.PublishModeHost && port.PublishedPort != 0 {
				portSpec := hostPortSpec{protocol: port.Protocol, publishedPort: port.PublishedPort}
				nodeInfo.usedHostPorts[portSpec] = struct{}{}
			}
		}
	}

	// check if the Task already has devices assigned. if so, just reassign
	// those devices. we should only have entries in t.Devices if the task
	// actually has devices assigned.
	if len(t.Devices) > 0 {
		// if this is the case, all we need to do is update the nodeDevices to
		// reflect current device assignment
		for _, device := range t.Devices {
			nodeInfo.nodeDevices[device.PathOnHost] = t.ID
		}
	} else {
		// claim all of the devices we're using. we already know that this node
		// has enough free devices, because it's already passed all the
		// filters.
		for _, device := range t.Spec.Devices {
			if device == nil {
				// device should never be nil, but skip if it is so we don't
				// segfault
				continue
			}
			// now, get the set of all node devices in this class, and go
			// through each of them
			for dev := range nodeInfo.nodeDeviceClasses[device.DeviceClassID] {
				// find the task that this device is in use by.
				inUseBy := nodeInfo.nodeDevices[dev]
				if inUseBy == "" {
					// if this device isn't in use, then set it for this task.
					nodeInfo.nodeDevices[dev] = t.ID

					deviceAttachment := &api.DeviceAttachment{
						DeviceClassID:     device.DeviceClassID,
						DeviceCgroupRules: device.DeviceCgroupRules,
						PathInTask:        device.Path,
						PathOnHost:        dev,
					}
					// additionally, add the device assignments to the task.
					// this is safe even if t.Devices is nil because append
					// works on nil slices
					t.Devices = append(t.Devices, deviceAttachment)
					// finally, break out of this loop, because we've found a
					// device for this task.
					break
				}
				// otherwise, we'll be continuing to the next device on the
				// node
			}
		}
	}

	if t.DesiredState <= api.TaskStateRunning {
		nodeInfo.ActiveTasksCount++
		nodeInfo.ActiveTasksCountByService[t.ServiceID]++
	}

	return true
}

func taskReservations(spec api.TaskSpec) (reservations api.Resources) {
	if spec.Resources != nil && spec.Resources.Reservations != nil {
		reservations = *spec.Resources.Reservations
	}
	return
}

func (nodeInfo *NodeInfo) cleanupFailures(now time.Time) {
entriesLoop:
	for key, failuresEntry := range nodeInfo.recentFailures {
		for _, timestamp := range failuresEntry {
			if now.Sub(timestamp) < monitorFailures {
				continue entriesLoop
			}
		}
		delete(nodeInfo.recentFailures, key)
	}
	nodeInfo.lastCleanup = now
}

// taskFailed records a task failure from a given service.
func (nodeInfo *NodeInfo) taskFailed(ctx context.Context, t *api.Task) {
	expired := 0
	now := time.Now()

	if now.Sub(nodeInfo.lastCleanup) >= monitorFailures {
		nodeInfo.cleanupFailures(now)
	}

	versionedService := versionedService{serviceID: t.ServiceID}
	if t.SpecVersion != nil {
		versionedService.specVersion = *t.SpecVersion
	}

	for _, timestamp := range nodeInfo.recentFailures[versionedService] {
		if now.Sub(timestamp) < monitorFailures {
			break
		}
		expired++
	}

	if len(nodeInfo.recentFailures[versionedService])-expired == maxFailures-1 {
		log.G(ctx).Warnf("underweighting node %s for service %s because it experienced %d failures or rejections within %s", nodeInfo.ID, t.ServiceID, maxFailures, monitorFailures.String())
	}

	nodeInfo.recentFailures[versionedService] = append(nodeInfo.recentFailures[versionedService][expired:], now)
}

// countRecentFailures returns the number of times the service has failed on
// this node within the lookback window monitorFailures.
func (nodeInfo *NodeInfo) countRecentFailures(now time.Time, t *api.Task) int {
	versionedService := versionedService{serviceID: t.ServiceID}
	if t.SpecVersion != nil {
		versionedService.specVersion = *t.SpecVersion
	}

	recentFailureCount := len(nodeInfo.recentFailures[versionedService])
	for i := recentFailureCount - 1; i >= 0; i-- {
		if now.Sub(nodeInfo.recentFailures[versionedService][i]) > monitorFailures {
			recentFailureCount -= i + 1
			break
		}
	}

	return recentFailureCount
}
