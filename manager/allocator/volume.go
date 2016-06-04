package allocator

import (
	"fmt"

	"github.com/docker/go-events"
	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/log"
	"github.com/docker/swarm-v2/manager/allocator/volumeallocator"
	"github.com/docker/swarm-v2/manager/state"
	"github.com/docker/swarm-v2/manager/state/store"
	"golang.org/x/net/context"
)

const (
	// Volume allocator Voter ID for task allocation vote.
	volumeVoter = "volume"
)

// Volume context information which is used in the volume allocation code.
type volumeContext struct {
	// Instance of the volume allocator which tracks cluster-wide volume allocations
	volAllocator *volumeallocator.VolumeAllocator

	// A table of unallocated tasks which will be revisited if any thing
	// changes in system state that might help task allocation.
	// For example, when a new volume is added to the cluster, we may be able to
	// schedule a Task which uses that volume
	unallocatedTasks map[string]*api.Task
}

// Init function registered with the allocator
func (a *Allocator) doVolumeInit(ctx context.Context) error {
	va, err := volumeallocator.New()
	if err != nil {
		return err
	}

	vc := &volumeContext{
		volAllocator:     va,
		unallocatedTasks: make(map[string]*api.Task),
	}

	// Fetch existing volumes from the store before we started watching.
	var volumes []*api.Volume
	a.store.View(func(tx store.ReadTx) {
		volumes, err = store.FindVolumes(tx, store.All)
	})
	if err != nil {
		return fmt.Errorf("error listing all volumes in store while trying to allocate during init: %v", err)
	}

	// Cache clusterwide volumes in the volume allocator
	for _, vol := range volumes {
		va.AddVolumeToCache(vol)
	}

	// Process tasks in the store so far before we started watching.
	var tasks []*api.Task
	a.store.View(func(tx store.ReadTx) {
		tasks, err = store.FindTasks(tx, store.All)
	})
	if err != nil {
		return fmt.Errorf("error listing all tasks in store while trying to allocate during init: %v", err)
	}

	if _, err := a.store.Batch(func(batch *store.Batch) error {
		for _, t := range tasks {
			if taskDead(t) {
				continue
			}

			// No container or volume configured. Try
			// voting to move it to allocated state.
			if t.GetContainer() == nil || len(t.GetContainer().Spec.Mounts) == 0 {
				if t.Status.State >= api.TaskStateAllocated {
					continue
				}
				// Update the Task
				if a.taskAllocateVote(volumeVoter, t.ID) {
					if err := batch.Update(func(tx store.Tx) error {
						storeT := store.GetTask(tx, t.ID)
						if storeT == nil {
							return fmt.Errorf("task %s not found while trying to update state", t.ID)
						}

						updateTaskStatus(storeT, api.TaskStateAllocated, "allocated")
						vc.volAllocator.AllocateTask(t)

						if err := store.UpdateTask(tx, storeT); err != nil {
							return fmt.Errorf("failed updating state in store transaction for task %s: %v", storeT.ID, err)
						}

						return nil
					}); err != nil {
						log.G(ctx).WithError(err).Error("error updating task volume")
					}
				}

				continue
			}

			err := batch.Update(func(tx store.Tx) error {
				_, err := a.allocateTaskVolumes(ctx, vc, tx, t)
				return err
			})
			if err != nil {
				log.G(ctx).Errorf("failed allocating task %s during init: %v", t.ID, err)
				vc.unallocatedTasks[t.ID] = t
			} else {
				vc.volAllocator.AllocateTask(t)
			}
		}

		return nil
	}); err != nil {
		return err
	}

	a.volCtx = vc
	return nil
}

func (a *Allocator) doVolumeAlloc(ctx context.Context, ev events.Event) {
	vc := a.volCtx

	switch v := ev.(type) {
	case state.EventCreateVolume:
		vol := v.Volume.Copy()
		if vc.volAllocator.FindClusterVolume(vol.Spec.Annotations.Name) != nil {
			break
		}

		vc.volAllocator.AddVolumeToCache(vol)
		a.procUnallocatedTasksVolume(ctx, vc)
	case state.EventDeleteVolume:
		vol := v.Volume.Copy()
		vc.volAllocator.RemoveVolumeFromCache(vol.Spec.Annotations.Name)
	case state.EventCreateTask, state.EventUpdateTask, state.EventDeleteTask:
		a.processTaskEvents(ctx, vc, ev)
	case state.EventCommit:
		a.procUnallocatedTasksVolume(ctx, vc)
		return
	}
}

func (a *Allocator) procUnallocatedTasksVolume(ctx context.Context, vc *volumeContext) {
	tasks := []*api.Task{}

	committed, err := a.store.Batch(func(batch *store.Batch) error {
		for _, t := range vc.unallocatedTasks {
			var allocatedT *api.Task
			err := batch.Update(func(tx store.Tx) error {
				var err error
				allocatedT, err = a.allocateTaskVolumes(ctx, vc, tx, t)
				return err
			})
			if err != nil {
				log.G(ctx).Errorf("task allocation failure: %v", err)
				continue
			}

			tasks = append(tasks, allocatedT)
		}

		return nil
	})
	if err != nil {
		log.G(ctx).WithError(err).Error("failed to allocate volume to task")
	}

	var retryCnt int
	for len(tasks) != 0 {
		var err error

		for _, t := range tasks[:committed] {
			vc.volAllocator.AllocateTask(t)
			delete(vc.unallocatedTasks, t.ID)
		}

		tasks = tasks[committed:]
		if len(tasks) == 0 {
			break
		}

		updatedTasks := []*api.Task{}
		committed, err = a.store.Batch(func(batch *store.Batch) error {
			for _, t := range tasks {
				err := batch.Update(func(tx store.Tx) error {
					return store.UpdateTask(tx, t)
				})

				if err != nil {
					log.G(ctx).WithError(err).Error("allocated task store update failure")
					continue
				}

				updatedTasks = append(updatedTasks, t)
			}

			return nil
		})
		if err != nil {
			log.G(ctx).WithError(err).Error("failed a store batch operation while processing unallocated tasks")
		}

		tasks = updatedTasks

		select {
		case <-ctx.Done():
			return
		default:
		}

		retryCnt++
		if retryCnt >= 3 {
			log.G(ctx).Errorf("failed to complete batch update of allocated tasks after 3 retries")
			break
		}
	}
}

func (a *Allocator) allocateTaskVolumes(ctx context.Context, vc *volumeContext, tx store.Tx, t *api.Task) (*api.Task, error) {
	if vc.volAllocator.IsTaskAllocated(t) {
		return t, nil
	}

	vols := map[string]*api.Volume{}
	hasUnboundVolumes := false
	for _, m := range t.GetContainer().Spec.Mounts {
		if m.Type != api.MountTypeVolume {
			continue
		}
		clusterWideVolume := vc.volAllocator.FindClusterVolume(m.VolumeName)

		// Has the clusterwide volume not been defined yet?
		if clusterWideVolume == nil {
			hasUnboundVolumes = true
			continue
		}
		vols[m.VolumeName] = clusterWideVolume
	}

	// Now remove those volumes from tv.vols that have already have an entry in PluginVolumes
	// PluginVolumes is part of the "bound" runtime state for volumes
	for _, alloc := range t.GetContainer().Volumes {
		if _, exists := vols[alloc.Spec.DriverConfig.Name]; exists {
			delete(vols, alloc.Spec.DriverConfig.Name)
		}
	}

	// Bind Volumes that can be bound
	var pluginVols = make([]*api.Volume, 0)
	for _, vol := range vols {
		pv := vol.Copy()
		pluginVols = append(pluginVols, pv)
	}

	// Get the latest task state from the store before updating.
	storeT := store.GetTask(tx, t.ID)
	if storeT == nil {
		return nil, fmt.Errorf("could not find task %s while trying to update volume allocation", t.ID)
	}

	// Update the volume allocations and moving to
	// ALLOCATED state on top of the latest store state.

	// Don't move task to allocated state if there are still unbound volumes
	if !hasUnboundVolumes && a.taskAllocateVote(volumeVoter, t.ID) {
		updateTaskStatus(storeT, api.TaskStateAllocated, "allocated")
	}

	// Append the new PluginVolumes to the existing array of PluginVolumes
	storeT.GetContainer().Volumes = append(storeT.GetContainer().Volumes, pluginVols...)
	if err := store.UpdateTask(tx, storeT); err != nil {
		return nil, fmt.Errorf("failed updating state in store transaction for task %s: %v", storeT.ID, err)
	}

	if hasUnboundVolumes {
		return nil, fmt.Errorf("failed to bind all volumes for task %s", t.ID)
	}
	return t, nil
}

func (a *Allocator) processTaskEvents(ctx context.Context, vc *volumeContext, ev events.Event) {
	var (
		isDelete bool
		t        *api.Task
	)

	switch v := ev.(type) {
	case state.EventCreateTask:
		t = v.Task.Copy()
	case state.EventUpdateTask:
		t = v.Task.Copy()
	case state.EventDeleteTask:
		isDelete = true
		t = v.Task.Copy()
	}

	// If the task has stopped running or it's being deleted then
	// we should remove it from unallocatedTasks
	if taskDead(t) || isDelete {
		vc.volAllocator.DeallocateTask(t)

		// Cleanup any task references that might exist in unallocatedTasks
		delete(vc.unallocatedTasks, t.ID)
		return
	}

	// Task has no volume attached and it is created for a
	// service which has no volume configuration. Move it to
	// ALLOCATED state.
	if t.GetContainer() == nil || len(t.GetContainer().Spec.Mounts) == 0 {
		// If we are already in allocated state, there is
		// absolutely nothing else to do.
		if t.Status.State >= api.TaskStateAllocated {
			return
		}

		// Update the Task
		if a.taskAllocateVote(volumeVoter, t.ID) {
			if err := a.store.Update(func(tx store.Tx) error {
				storeT := store.GetTask(tx, t.ID)
				if storeT == nil {
					return fmt.Errorf("task %s not found while trying to update state", t.ID)
				}

				updateTaskStatus(storeT, api.TaskStateAllocated, "allocated")
				vc.volAllocator.AllocateTask(t)

				if err := store.UpdateTask(tx, storeT); err != nil {
					return fmt.Errorf("failed updating state in store transaction for task %s: %v", storeT.ID, err)
				}

				return nil
			}); err != nil {
				log.G(ctx).WithError(err).Error("error updating task volume")
			}
		}
		return
	}

	if !vc.volAllocator.IsTaskAllocated(t) {
		vc.unallocatedTasks[t.ID] = t
	}
}
