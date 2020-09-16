package csi

import (
	"context"
	"sync"

	"github.com/docker/go-events"

	"github.com/docker/swarmkit/api"
	"github.com/docker/swarmkit/manager/state/store"
	// "github.com/container-storage-interface/spec/lib/go/csi"
)

// Scheduler is an interface covering the methods on the volume manager used by
// the swarmkit scheduler
type Scheduler interface {
	// ReserveVolume marks a volume as in use.
	ReserveVolume(volumeID, taskID, nodeID string, readOnly bool)
	// UnreserveVolume removes a reservation.
	UnreserveVolume(volumeID, taskID string)
}

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
	// it is being used. if a volume is not yet being used, it may not be
	// present in this map.
	volumes map[string]*volumeStatus
	// volumesMu guards access to the volumes map
	volumesMu sync.Mutex
}

func NewManager(s *store.MemoryStore) *Manager {
	return &Manager{
		store:     s,
		stopChan:  make(chan struct{}),
		doneChan:  make(chan struct{}),
		newPlugin: NewPlugin,
		plugins:   map[string]Plugin{},
		provider:  NewSecretProvider(s),
		volumes:   map[string]*volumeStatus{},
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
	// TODO(dperny): instead of executing directly off of the event stream, add
	// objects received to some kind of intermediate structure, so we can
	// easily retry.
	switch e := ev.(type) {
	case api.EventUpdateCluster:
		// TODO(dperny): verify that the Cluster in this event can never be nil
		vm.cluster = e.Cluster
		vm.updatePlugins()
	case api.EventCreateVolume:
		vm.createVolume(e.Volume)
	case api.EventUpdateVolume:
		vm.handleVolume(e.Volume.ID)
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

// TODO(dperny): add volumes updated to a map and process in batches
func (vm *Manager) handleVolume(id string) {
	var volume *api.Volume
	vm.store.View(func(tx store.ReadTx) {
		volume = store.GetVolume(tx, id)
	})
	if volume == nil {
		return
	}

	updated := false
	for _, status := range volume.PublishStatus {
		if status.State == api.VolumePublishStatus_PENDING_PUBLISH {
			plug := vm.plugins[volume.Spec.Driver.Name]
			publishContext, err := plug.PublishVolume(context.TODO(), volume, status.NodeID)
			if err == nil {
				// TODO(dperny): handle error
				status.State = api.VolumePublishStatus_PUBLISHED
				status.PublishContext = publishContext
				updated = true
			}
		}
	}

	if updated {
		if err := vm.store.Update(func(tx store.Tx) error {
			// the publish status is now authoritative. read-update-write the
			// volume object.
			v := store.GetVolume(tx, volume.ID)
			if v == nil {
				// volume should never be deleted with pending publishes.
				// either handle this error otherwise document why we don't.
				return nil
			}

			v.PublishStatus = volume.PublishStatus
			return store.UpdateVolume(tx, v)
		}); err != nil {
			// TODO(dperny): handle error
		}
	}
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
