package csi

import (
	"context"
	"sync"

	"github.com/docker/go-events"

	"github.com/docker/swarmkit/api"
	"github.com/docker/swarmkit/manager/state/store"
	// "github.com/container-storage-interface/spec/lib/go/csi"
)

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
