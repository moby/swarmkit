package volumes

import (
	"sync"

	"github.com/docker/go-events"

	"github.com/docker/swarmkit/api"
	"github.com/docker/swarmkit/manager/state/store"
	// "github.com/container-storage-interface/spec/lib/go/csi"
)

type VolumeManager struct {
	store *store.MemoryStore

	// synchronization for starting and stopping the VolumeManager
	startOnce sync.Once

	stopChan chan struct{}
	stopOnce sync.Once
	doneChan chan struct{}

	plugins map[string]*Plugin
}

func NewVolumeManager(s *store.MemoryStore) *VolumeManager {
	return &VolumeManager{
		store:    s,
		stopChan: make(chan struct{}),
		doneChan: make(chan struct{}),
		plugins:  map[string]*Plugin{},
	}
}

func (vm *VolumeManager) Run() {
	vm.startOnce.Do(func() {
		vm.run()
	})
}

func (vm *VolumeManager) run() {
	defer close(vm.doneChan)
	for {
		select {
		case <-vm.stopChan:
			return
		}
	}
}

func (vm *VolumeManager) init() {

}

func (vm *VolumeManager) Stop() {
	vm.stopOnce.Do(func() {
		close(vm.stopChan)
	})

	<-vm.doneChan
}

func (vm *VolumeManager) handleEvent(ev events.Event) {
	switch e := ev.(type) {
	case api.EventCreateVolume:
		vm.createVolume(e.Volume)
	}
}

func (vm *VolumeManager) createVolume(v *api.Volume) {
	/*
		p, ok := vm.plugins[v.Spec.Driver.Name]
		if !ok {
			// TODO(dperny): log something
			return
		}
	*/
}
