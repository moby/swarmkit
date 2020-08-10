package csi

import (
	"context"
	"strings"
	"sync"

	"github.com/docker/go-events"

	"github.com/docker/swarmkit/api"
	"github.com/docker/swarmkit/manager/state/store"
	// "github.com/container-storage-interface/spec/lib/go/csi"
)

// volumeStatus wraps the ephemeral status of a volume
//
// TODO(dperny): keeping the whole task object here may be... not ideal.
type volumeStatus struct {
	// tasks is a mapping of task IDs using this volume to how this volume is
	// being used by that task.
	tasks map[string]volumeUsage
}

type volumeUsage struct {
	nodeID   string
	readOnly bool
}

type Manager struct {
	store *store.MemoryStore
	// provider is the SecretProvider which allows retrieving secrets. Used
	// when creating new Plugin objects.
	provider SecretProvider

	// newPlugin is a function which returns an object implementing the Plugin
	// interface. It allows us to swap out the implementation of plugins while
	// unit-testing the Manager
	newPlugin func(config *api.CSIConfig_Plugin, provider SecretProvider) Plugin

	// synchronization for starting and stopping the Manager
	startOnce sync.Once

	stopChan chan struct{}
	stopOnce sync.Once
	doneChan chan struct{}

	cluster *api.Cluster
	plugins map[string]Plugin

	// volumes contains information about how volumes are currently being used
	// by the system. This includes which tasks a volume is in use by, and how
	// it is being used.
	volumes map[string]*volumeStatus
}

func NewManager(s *store.MemoryStore) *Manager {
	return &Manager{
		store:     s,
		stopChan:  make(chan struct{}),
		doneChan:  make(chan struct{}),
		newPlugin: NewPlugin,
		plugins:   map[string]Plugin{},
		provider:  NewSecretProvider(s),
	}
}

func (vm *Manager) Run() {
	vm.startOnce.Do(func() {
		vm.run()
	})
}

func (vm *Manager) run() {
	defer close(vm.doneChan)

	watch, cancel, err := store.ViewAndWatch(vm.store, func(tx store.ReadTx) error {
		cluster, err := store.FindClusters(tx, store.ByName(store.DefaultClusterName))
		if err != nil {
			return err
		}
		vm.cluster = cluster[0]
		return nil
	})
	if err != nil {
		// TODO(dperny): log message
		return
	}
	defer cancel()

	vm.init()

	for {
		select {
		case ev := <-watch:
			vm.handleEvent(ev)
		case <-vm.stopChan:
			return
		}
	}
}

// init does one-time setup work for the Manager, like creating all of
// the Plugins and initializing the local state of the component.
func (vm *Manager) init() {
	vm.updatePlugins()

	var nodes []*api.Node
	vm.store.View(func(tx store.ReadTx) {
		var err error
		nodes, err = store.FindNodes(tx, store.All)
		if err != nil {
			// TODO(dperny): log something
		}
	})

	for _, node := range nodes {
		vm.handleNode(node)
	}
}

func (vm *Manager) updatePlugins() {
	// activePlugins is a set of plugin names that are currently in the cluster
	// spec. this lets remove from the vm.plugins map any plugins that are
	// no longer in use.
	activePlugins := map[string]struct{}{}

	if vm.cluster != nil {
		for _, plugin := range vm.cluster.Spec.CSIConfig.Plugins {
			// it's exceedingly unlikely that plugin could ever be nil but
			// better this than segfault
			if plugin != nil {
				if _, ok := vm.plugins[plugin.Name]; !ok {
					vm.plugins[plugin.Name] = vm.newPlugin(plugin, vm.provider)
				}
				activePlugins[plugin.Name] = struct{}{}
			}
		}
	}

	// remove any plugins that are no longer in use.
	for pluginName := range vm.plugins {
		if _, ok := activePlugins[pluginName]; !ok {
			delete(vm.plugins, pluginName)
		}
	}
}

func (vm *Manager) Stop() {
	vm.stopOnce.Do(func() {
		close(vm.stopChan)
	})

	<-vm.doneChan
}

func (vm *Manager) handleEvent(ev events.Event) {
	switch e := ev.(type) {
	case api.EventUpdateCluster:
		// TODO(dperny): verify that the Cluster in this event can never be nil
		vm.cluster = e.Cluster
		vm.updatePlugins()
	case api.EventCreateVolume:
		vm.createVolume(e.Volume)
	case api.EventCreateNode:
		vm.handleNode(e.Node)
	case api.EventUpdateNode:
		// for updates, we're only adding the node to every plugin. if the node
		// no longer reports CSIInfo for a specific plugin, we will just leave
		// the stale data in the plugin. this should not have any adverse
		// effect, because the memory impact is small, and this operation
		// should not be frequent. this may change as the code for volumes
		// becomes more polished.
		vm.handleNode(e.Node)
	case api.EventDeleteNode:
		vm.handleNodeRemove(e.Node.ID)
	}
}

func (vm *Manager) createVolume(v *api.Volume) {
	p, ok := vm.plugins[v.Spec.Driver.Name]
	if !ok {
		// TODO(dperny): log something
		return
	}

	info, err := p.CreateVolume(context.Background(), v)
	if err != nil {
		// TODO(dperny) log or handle errors
		return
	}

	// TODO(dperny): handle error
	vm.store.Update(func(tx store.Tx) error {
		v2 := store.GetVolume(tx, v.ID)
		// TODO(dperny): handle missing volume
		if v2 == nil {
			return nil
		}

		v2.VolumeInfo = info

		return store.UpdateVolume(tx, v2)
	})
}

// handleNode handles one node event
func (vm *Manager) handleNode(n *api.Node) {
	if n.Description == nil {
		return
	}
	// we just call AddNode on every update. Because it's just a map
	// assignment, this is probably faster than checking if something changed.
	for _, info := range n.Description.CSIInfo {
		p, ok := vm.plugins[info.PluginName]
		if !ok {
			// TODO(dperny): log something
			continue
		}
		p.AddNode(n.ID, info.NodeID)
	}
}

// handleNodeRemove handles a node delete event
func (vm *Manager) handleNodeRemove(nodeID string) {
	// we just call RemoveNode on every plugin, because it's probably quicker
	// than checking if the node was using that plugin.
	for _, plugin := range vm.plugins {
		plugin.RemoveNode(nodeID)
	}
}

// GetVolumes takes either a volume Name, or a volume Group (prefixed by
// "group:") and returns a slice of volumes matching that name or group.
func (vm *Manager) GetVolumes(source string) []*api.Volume {
	// whether we have one volume specified by name, or many volumes specified
	// by group, we'll use a slice of volume objects to simplify the code path.
	volumes := []*api.Volume{}

	vm.store.View(func(tx store.ReadTx) {
		// try trimming off the "group:" prefix. if the resulting string is
		// different from the input string (meaning something has been trimmed),
		// then this volume is actually a volume group.
		if group := strings.TrimPrefix(source, "group:"); group != source {
			// get all of the volumes in a group

			// then, check if any volume in that group can work on this node, with
			// the specific requirements of the task.

			// if so, mark off this volume, so we don't reuse it to satisfy another
			// mount requirement
			// we should never get an error. errors should only happen in Find if
			// the By is invalid, which shouldn't happen because it is statically
			// chosen.
			volumes, _ = store.FindVolumes(tx, store.ByVolumeGroup(group))
		} else {
			volumes, _ = store.FindVolumes(tx, store.ByName(source))
		}
	})
	return volumes
}

// IsVolumeAvailableOnNode checks if a mount can be satisfied by a volume on
// the given node.
//
// Returns the ID of the volume that satisfies this mount, or an empty string
// if no volume does
func (vm *Manager) IsVolumeAvailableOnNode(mount *api.Mount, node *api.Node) string {
	volumes := vm.GetVolumes(mount.Source)

	// if at any point, we find a volume in the group that meets the
	// requirements, we will return the ID of the volume  straight out of the
	// loop.  otherwise, if we run through the whole loop, then no volume works
	// on this node.
	for _, volume := range volumes {
		if vm.isVolumeAvailable(volume, node, mount.ReadOnly) {
			return volume.ID
		}
	}

	return ""
}

// isVolumeAvailable is a helper method that checks if the single given
// volume is usable as specified in the mount on the given node. This helper
// works on a single volume, where IsVolumeAvailableOnNode can also work on a
// group
func (vm *Manager) isVolumeAvailable(volume *api.Volume, node *api.Node, readOnly bool) bool {
	// a volume is not available if it has not yet passed through the creation
	// step.
	if volume.VolumeInfo == nil || volume.VolumeInfo.VolumeID == "" {
		return false
	}
	// get the node topology for this volume
	var top *api.Topology
	// get the topology for this volume's driver on this node
	for _, info := range node.Description.CSIInfo {
		if info.PluginName == volume.Spec.Driver.Name {
			top = info.AccessibleTopology
		}
	}

	// check if the volume is available on this node. a volume's
	// availability on a node depends on its accessible topology, how it's
	// already being used, and how this task intends to use it.

	// check if the volume is already in use.
	status, ok := vm.volumes[volume.ID]

	// if the volume is in use, and its scope is single-node, we can only
	// schedule to this node.
	if ok && volume.Spec.AccessMode.Scope == api.VolumeScopeSingleNode {
		// if the volume is not in use on this node already, then it can't
		// be used here.
		for _, task := range status.tasks {
			if task.nodeID != node.ID {
				return false
			}
		}
	}

	// even if the volume is currently on this node, or it has multi-node
	// access, the volume sharing needs to be compatible.
	switch volume.Spec.AccessMode.Sharing {
	case api.VolumeSharingNone:
		// if the volume sharing is none, then the volume cannot be
		// used by another task
		if ok && len(status.tasks) > 0 {
			return false
		}
	case api.VolumeSharingOneWriter:
		// if the mount is not ReadOnly, and the volume has a writer, then
		// we this volume does not work.
		if !readOnly && hasWriter(status) {
			return false
		}
	case api.VolumeSharingReadOnly:
		// if the volume sharing is read-only, then the Mount must also
		// be read-only
		if !readOnly {
			return false
		}
	}

	// then, do the quick check of whether this volume is in the topology.  if
	// the volume has an AccessibleTopology, and it does not lie within the
	// node's topology, then this volume won't fit.
	if !IsInTopology(top, volume.VolumeInfo.AccessibleTopology) {
		return false
	}

	return true
}

// hasWriter is a helper function that returns true if at least one task is
// using this volume not in read-only mode.
func hasWriter(status *volumeStatus) bool {
	if status == nil {
		return false
	}
	for _, task := range status.tasks {
		if !task.readOnly {
			return true
		}
	}
	return false
}
