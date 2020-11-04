package csi

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/docker/swarmkit/agent/csi/plugin"
	"github.com/docker/swarmkit/agent/exec"
	"github.com/docker/swarmkit/api"
	"github.com/docker/swarmkit/log"
)

// volumes is a map that keeps all the currently available volumes to the agent
// mapped by volume ID.
type volumes struct {
	mu sync.RWMutex                     // To sync map "m" and "pluginMap"
	m  map[string]*api.VolumeAssignment // Map between VolumeID and VolumeAssignment

	plugins plugin.PluginManager

	// tryVolumesCtx is a context for retrying volume operations.
	tryVolumesCtx context.Context
	// tryVolumesCancel is the cancel func for tryVolumesCtx
	tryVolumesCancel context.CancelFunc
}

const maxRetries int = 20

const initialBackoff = 1 * time.Millisecond

// NewManager returns a place to store volumes.
func NewManager() exec.VolumesManager {
	ctx, cancel := context.WithCancel(context.TODO())
	return &volumes{
		m:                map[string]*api.VolumeAssignment{},
		plugins:          plugin.NewPluginManager(),
		tryVolumesCtx:    ctx,
		tryVolumesCancel: cancel,
	}
}

// Get returns a volume published path for the provided volume ID.  If the volume doesn't exist, returns empty string.
func (r *volumes) Get(volumeID string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	ctx := context.Background()
	if volume, ok := r.m[volumeID]; ok {
		if plugin, err := r.plugins.Get(volume.Driver.Name); err == nil {
			path := plugin.GetPublishedPath(volumeID)
			if path != "" {
				return path, nil
			}
			log.G(ctx).WithField("method", "(*volumes).Get").Debugf("Path not published for volume:%v", volumeID)
		}
	}
	return "", fmt.Errorf("%w: published path is unavailable", exec.ErrDependencyNotReady)
}

// Add adds one or more volumes to the volume map.
func (r *volumes) Add(volumes ...api.VolumeAssignment) {
	r.mu.Lock()
	defer r.mu.Unlock()

	var volumeObjects []*api.VolumeAssignment
	ctx := context.Background()
	for _, volume := range volumes {
		v := volume.Copy()
		log.G(ctx).WithField("method", "(*volumes).Add").Debugf("Add Volume: %v", volume.VolumeID)

		r.m[volume.VolumeID] = v
		volumeObjects = append(volumeObjects, v)
	}
	go r.iterateVolumes(volumeObjects, true)
}

func (r *volumes) iterateVolumes(volumeObjects []*api.VolumeAssignment, isAdd bool) {
	r.mu.Lock()
	ctx := r.tryVolumesCtx
	r.mu.Unlock()

	for _, v := range volumeObjects {
		if isAdd {
			r.tryAddVolume(ctx, v)
		} else {
			r.tryRemoveVolume(ctx, v)
		}
	}
}

func (r *volumes) tryAddVolume(ctx context.Context, assignment *api.VolumeAssignment) {

	driverName := assignment.Driver.Name

	var (
		plugin      plugin.NodePlugin
		pluginFound bool
		err         error
	)

	plugin, err = r.plugins.Get(driverName)
	pluginFound = err == nil

	if !pluginFound {
		err = fmt.Errorf("plugin %s not found", driverName)
	} else {
		err = plugin.NodeStageVolume(ctx, assignment)
	}

	if err != nil {
		waitFor := initialBackoff
	retryStage:
		for i := 0; i < maxRetries; i++ {
			log.G(ctx).WithError(err).Debugf("staging volume %s failed, retrying", assignment.ID)
			select {
			case <-ctx.Done():
				// selecting on ctx.Done() allows us to bail out of retrying early
				return
			case <-time.After(waitFor):
				// time.After is better than using time.Sleep, because it blocks
				// on a channel read, rather than suspending the whole
				// goroutine. That lets us do the above check on ctx.Done().
				//
				// time.After is convenient, but it has a key problem: the timer
				// is not garbage collected until the channel fires. this
				// shouldn't be a problem, unless the context is canceled, there
				// is a very long timer, and there are a lot of other goroutines
				// in the same situation.

				// we may still be waiting on the plugin to become available.
				// if this is the case, then we should check if it's yet
				// available.
				if !pluginFound {
					plugin, err = r.plugins.Get(driverName)
					pluginFound = err == nil

					if !pluginFound {
						err = fmt.Errorf("plugin %s not found", driverName)
						continue retryStage
					}
				}

				err = plugin.NodeStageVolume(ctx, assignment)
				if err == nil {
					break retryStage
				}
			}
			// if the exponential factor is 2, you can avoid using floats by
			// doing bit shifts. each shift left increases the number by a power
			// of 2. we can do this because Duration is ultimately int64.
			waitFor = waitFor << 1
			// clamp the value of waitFor at 5 minutes, though. any longer
			// would be basically useless to us
			if waitFor > (5 * time.Minute) {
				waitFor = 5 * time.Minute
			}
		}
		// if we've gone through the whole retry loop and the error is still
		// nil, then there is nothing else to be tried.
		if err != nil {
			return
		}
	}

	// we know, now, that the plugin is available, because we checked in the
	// stage loop for it.
	if err := plugin.NodePublishVolume(ctx, assignment); err != nil {
		waitFor := initialBackoff
	retryPublish:
		for i := 0; i < maxRetries; i++ {
			select {
			case <-ctx.Done():
				return
			case <-time.After(waitFor):
				if err := plugin.NodePublishVolume(ctx, assignment); err == nil {
					break retryPublish
				}
			}
			waitFor = waitFor << 1
		}
	}
}

// TODO(ameyag): Cancel existing tryAddVolume when we try to remove a volume
func (r *volumes) tryRemoveVolume(ctx context.Context, assignment *api.VolumeAssignment) {
	return

	/*
		r.mu.RLock()
		plugin, ok := r.pluginMap[assignment.VolumeID]
		r.mu.RUnlock()
		if !ok {
			log.G(ctx).Debugf("plugin not found for VolumeID:%v", assignment.VolumeID)
			return
		}
		if err := plugin.NodeUnpublishVolume(ctx, assignment); err != nil {
			waitFor := initialBackoff
		retryUnPublish:
			for i := 0; i < maxRetries; i++ {
				select {
				case <-ctx.Done():
					return
				case <-time.After(waitFor):
					if err := plugin.NodeUnpublishVolume(ctx, assignment); err == nil {
						break retryUnPublish
					}
				}
				waitFor = waitFor << 1
			}
		}

		// Unstage
		if err := plugin.NodeUnstageVolume(ctx, assignment); err != nil {
			waitFor := initialBackoff
		retryUnstage:
			for i := 0; i < maxRetries; i++ {
				select {
				case <-ctx.Done():
					return
				case <-time.After(waitFor):
					if err := plugin.NodeUnstageVolume(ctx, assignment); err == nil {
						break retryUnstage
					}
				}
				waitFor = waitFor << 1
			}
		}
	*/
}

// Remove removes one or more volumes by ID from the volumes map. Succeeds
// whether or not the given IDs are in the map.
func (r *volumes) Remove(volumes []string) {
	// noop
}

// Reset removes all the volumes.
func (r *volumes) Reset() {
	// no-op
}

func (r *volumes) removeVolumes(volumeObjects []*api.VolumeAssignment) {
	ctx := context.Background()
	for _, v := range volumeObjects {
		go r.tryRemoveVolume(ctx, v)
	}
}

func (r *volumes) Plugins() exec.VolumePluginManager {
	return r.plugins
}

// taskRestrictedVolumesProvider restricts the ids to the task.
type taskRestrictedVolumesProvider struct {
	volumes   exec.VolumeGetter
	volumeIDs map[string]struct{}
}

func (sp *taskRestrictedVolumesProvider) Get(volumeID string) (string, error) {
	if _, ok := sp.volumeIDs[volumeID]; !ok {
		return "", fmt.Errorf("task not authorized to access volume %s", volumeID)
	}

	return sp.volumes.Get(volumeID)
}

// Restrict provides a getter that only allows access to the volumes
// referenced by the task.
func Restrict(volumes exec.VolumeGetter, t *api.Task) exec.VolumeGetter {
	vids := map[string]struct{}{}

	for _, v := range t.Volumes {
		vids[v.ID] = struct{}{}
	}

	return &taskRestrictedVolumesProvider{volumes: volumes, volumeIDs: vids}
}
