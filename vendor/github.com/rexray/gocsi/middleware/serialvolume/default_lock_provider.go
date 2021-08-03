package serialvolume

import (
	"context"
	"sync"

	"github.com/akutz/gosync"
)

type defaultLockProvider struct {
	volIDLocksL   sync.Mutex
	volNameLocksL sync.Mutex
	volIDLocks    map[string]gosync.TryLocker
	volNameLocks  map[string]gosync.TryLocker
}

func (i *defaultLockProvider) GetLockWithID(
	ctx context.Context, id string) (gosync.TryLocker, error) {

	i.volIDLocksL.Lock()
	defer i.volIDLocksL.Unlock()
	lock := i.volIDLocks[id]
	if lock == nil {
		lock = &gosync.TryMutex{}
		i.volIDLocks[id] = lock
	}
	return lock, nil
}

func (i *defaultLockProvider) GetLockWithName(
	ctx context.Context, name string) (gosync.TryLocker, error) {

	i.volNameLocksL.Lock()
	defer i.volNameLocksL.Unlock()
	lock := i.volNameLocks[name]
	if lock == nil {
		lock = &gosync.TryMutex{}
		i.volNameLocks[name] = lock
	}
	return lock, nil
}
