package dispatcher

import (
	"fmt"

	"github.com/docker/swarmkit/api"
	"github.com/docker/swarmkit/api/equality"
	"github.com/docker/swarmkit/api/validation"
	"github.com/docker/swarmkit/identity"
	"github.com/docker/swarmkit/manager/drivers"
	"github.com/docker/swarmkit/manager/state/store"
	"github.com/sirupsen/logrus"
)

type typeAndID struct {
	id      string
	objType api.ResourceType
}

type assignmentSet struct {
	dp                   *drivers.DriverProvider
	tasksMap             map[string]*api.Task
	tasksUsingDependency map[typeAndID]map[string]struct{}
	// pendingVolumes is a set of Volume IDs that should be assigned as part of
	// this set, but are waiting on the Published state.
	pendingVolumes map[string]*api.Volume
	changes        map[typeAndID]*api.AssignmentChange
	log            *logrus.Entry
}

func newAssignmentSet(log *logrus.Entry, dp *drivers.DriverProvider) *assignmentSet {
	return &assignmentSet{
		dp:                   dp,
		changes:              make(map[typeAndID]*api.AssignmentChange),
		tasksMap:             make(map[string]*api.Task),
		tasksUsingDependency: make(map[typeAndID]map[string]struct{}),
		pendingVolumes:       make(map[string]*api.Volume),
		log:                  log,
	}
}

func assignSecret(a *assignmentSet, readTx store.ReadTx, mapKey typeAndID, t *api.Task) {
	if _, exists := a.tasksUsingDependency[mapKey]; !exists {
		a.tasksUsingDependency[mapKey] = make(map[string]struct{})
	}
	secret, doNotReuse, err := a.secret(readTx, t, mapKey.id)
	if err != nil {
		a.log.WithFields(logrus.Fields{
			"resource.type": "secret",
			"secret.id":     mapKey.id,
			"error":         err,
		}).Debug("failed to fetch secret")
		return
	}
	// If the secret should not be reused for other tasks, give it a unique ID for the task to allow different values for different tasks.
	if doNotReuse {
		// Give the secret a new ID and mark it as internal
		originalSecretID := secret.ID
		taskSpecificID := identity.CombineTwoIDs(originalSecretID, t.ID)
		secret.ID = taskSpecificID
		secret.Internal = true
		// Create a new mapKey with the new ID and insert it into the dependencies map for the task.
		// This will make the changes map contain an entry with the new ID rather than the original one.
		mapKey = typeAndID{objType: mapKey.objType, id: secret.ID}
		a.tasksUsingDependency[mapKey] = make(map[string]struct{})
		a.tasksUsingDependency[mapKey][t.ID] = struct{}{}
	}
	a.changes[mapKey] = &api.AssignmentChange{
		Assignment: &api.Assignment{
			Item: &api.Assignment_Secret{
				Secret: secret,
			},
		},
		Action: api.AssignmentChange_AssignmentActionUpdate,
	}
}

func assignConfig(a *assignmentSet, readTx store.ReadTx, mapKey typeAndID) {
	a.tasksUsingDependency[mapKey] = make(map[string]struct{})
	config := store.GetConfig(readTx, mapKey.id)
	if config == nil {
		a.log.WithFields(logrus.Fields{
			"resource.type": "config",
			"config.id":     mapKey.id,
		}).Debug("config not found")
		return
	}
	a.changes[mapKey] = &api.AssignmentChange{
		Assignment: &api.Assignment{
			Item: &api.Assignment_Config{
				Config: config,
			},
		},
		Action: api.AssignmentChange_AssignmentActionUpdate,
	}
}

// assignVolume handles logic for assigning a given volume. It creates a
// VolumeAssignment if the Volume is already published and ready to go. If the
// Volume is not yet published, then it sets up the tasksUsingDependency map,
// but does not yet create the VolumeAssignment.
func assignVolume(a *assignmentSet, readTx store.ReadTx, mapKey typeAndID, t *api.Task) {
	a.tasksUsingDependency[mapKey] = make(map[string]struct{})
	volume := store.GetVolume(readTx, mapKey.id)
	if volume == nil {
		a.log.WithFields(logrus.Fields{
			"resource.type": "volume",
			"volume.id":     mapKey.id,
		}).Debug("volume not found")
		return
	}

	// the VolumeInfo is added when swarm calls CreateVolume on the plugin. It
	// should never be missing at this point, because we have to have
	// VolumeInfo before we can call schedule a volume.
	if volume.VolumeInfo == nil {
		a.log.WithFields(logrus.Fields{
			"resource.type": "volume",
			"volume.id":     mapKey.id,
		}).Debug("volume not ready (missing VolumeInfo)")
		return
	}

	// Volumes may need Secrets of their own to work properly. ensure that all
	// of the necessary Secrets are assigned to the node.
	for _, secret := range volume.Spec.Secrets {
		mapKey := typeAndID{objType: api.ResourceType_SECRET, id: secret.Secret}
		if len(a.tasksUsingDependency[mapKey]) == 0 {
			assignSecret(a, readTx, mapKey, t)
		}
		a.tasksUsingDependency[mapKey][t.ID] = struct{}{}
	}

	// we can infer the NodeID by just looking at the NodeID for the task
	nodeID := t.NodeID
	published := false
	for _, status := range volume.PublishStatus {
		if status.NodeID == nodeID && status.State == api.VolumePublishStatus_PUBLISHED {
			published = true
			break
		}
	}

	if !published {
		a.pendingVolumes[volume.ID] = volume
		return
	}

	// volumes are sent to nodes as VolumeAssignments. This is because a node
	// needs node-specific information (the PublishContext from
	// ControllerPublishVolume).
	assignment := &api.VolumeAssignment{
		ID:            volume.ID,
		VolumeID:      volume.VolumeInfo.VolumeID,
		Driver:        volume.Spec.Driver,
		VolumeContext: volume.VolumeInfo.VolumeContext,
		// TODO(dperny): PublishContext needs to be stored and retrieved. for
		// now we'll just skip it.
		PublishContext: map[string]string{},
		// TODO(dperny): sort out AccessMode.
		Secrets: volume.Spec.Secrets,
	}

	a.changes[mapKey] = &api.AssignmentChange{
		Assignment: &api.Assignment{
			Item: &api.Assignment_Volume{
				Volume: assignment,
			},
		},
		Action: api.AssignmentChange_AssignmentActionUpdate,
	}

	return
}

func (a *assignmentSet) addTaskDependencies(readTx store.ReadTx, t *api.Task) {
	// first, we go through all ResourceReferences, which give us the necessary
	// information about which secrets and configs are in use.
	for _, resourceRef := range t.Spec.ResourceReferences {
		mapKey := typeAndID{objType: resourceRef.ResourceType, id: resourceRef.ResourceID}
		// if there are no tasks using this dependency yet, then we can assign
		// it.
		if len(a.tasksUsingDependency[mapKey]) == 0 {
			switch resourceRef.ResourceType {
			case api.ResourceType_SECRET:
				assignSecret(a, readTx, mapKey, t)
			case api.ResourceType_CONFIG:
				assignConfig(a, readTx, mapKey)
			default:
				a.log.WithField(
					"resource.type", resourceRef.ResourceType,
				).Debug("invalid resource type for a task dependency, skipping")
				continue
			}
		}
		// otherwise, we don't need to add a new assignment. we just need to
		// track the fact that another task is now using this dependency.
		a.tasksUsingDependency[mapKey][t.ID] = struct{}{}
	}

	// then, we check volumes. volumes are different because they're not
	// contained in the ResourceReferences, they're instead contained in the
	// the VolumeAttachments.
	for _, volume := range t.Volumes {
		mapKey := typeAndID{objType: api.ResourceType_VOLUME, id: volume.ID}
		if len(a.tasksUsingDependency[mapKey]) == 0 {
			assignVolume(a, readTx, mapKey, t)
		}
	}

	var secrets []*api.SecretReference
	container := t.Spec.GetContainer()
	if container != nil {
		secrets = container.Secrets
	}

	for _, secretRef := range secrets {
		secretID := secretRef.SecretID
		mapKey := typeAndID{objType: api.ResourceType_SECRET, id: secretID}

		// This checks for the presence of each task in the dependency map for the
		// secret. This is currently only done for secrets since the other types of
		// dependencies do not support driver plugins. Arguably, the same task would
		// not have the same secret as a dependency more than once, but this check
		// makes sure the task only gets the secret assigned once.
		if _, exists := a.tasksUsingDependency[mapKey][t.ID]; !exists {
			assignSecret(a, readTx, mapKey, t)
		}
		a.tasksUsingDependency[mapKey][t.ID] = struct{}{}
	}

	var configs []*api.ConfigReference
	if container != nil {
		configs = container.Configs
	}
	for _, configRef := range configs {
		configID := configRef.ConfigID
		mapKey := typeAndID{objType: api.ResourceType_CONFIG, id: configID}

		if len(a.tasksUsingDependency[mapKey]) == 0 {
			assignConfig(a, readTx, mapKey)
		}
		a.tasksUsingDependency[mapKey][t.ID] = struct{}{}
	}
}

func (a *assignmentSet) releaseDependency(mapKey typeAndID, assignment *api.Assignment, taskID string) bool {
	delete(a.tasksUsingDependency[mapKey], taskID)
	if len(a.tasksUsingDependency[mapKey]) != 0 {
		return false
	}
	// No tasks are using the dependency anymore
	delete(a.tasksUsingDependency, mapKey)
	a.changes[mapKey] = &api.AssignmentChange{
		Assignment: assignment,
		Action:     api.AssignmentChange_AssignmentActionRemove,
	}
	return true
}

// releaseTaskDependencies needs a store transaction because volumes have
// associated Secrets which need to be released.
func (a *assignmentSet) releaseTaskDependencies(readTx store.ReadTx, t *api.Task) bool {
	var modified bool

	for _, resourceRef := range t.Spec.ResourceReferences {
		var assignment *api.Assignment
		switch resourceRef.ResourceType {
		case api.ResourceType_SECRET:
			assignment = &api.Assignment{
				Item: &api.Assignment_Secret{
					Secret: &api.Secret{ID: resourceRef.ResourceID},
				},
			}
		case api.ResourceType_CONFIG:
			assignment = &api.Assignment{
				Item: &api.Assignment_Config{
					Config: &api.Config{ID: resourceRef.ResourceID},
				},
			}
		default:
			a.log.WithField(
				"resource.type", resourceRef.ResourceType,
			).Debug("invalid resource type for a task dependency, skipping")
			continue
		}

		mapKey := typeAndID{objType: resourceRef.ResourceType, id: resourceRef.ResourceID}
		if a.releaseDependency(mapKey, assignment, t.ID) {
			modified = true
		}
	}

	// TODO(dperny): we need to ensure, affirmatively, that the volume has been
	// correctly wound down (by calling Unpublish and Unstage rpcs) before
	// fully releasing the assignment.
	for _, v := range t.Volumes {
		mapKey := typeAndID{objType: api.ResourceType_VOLUME, id: v.ID}
		assignment := &api.Assignment{
			Item: &api.Assignment_Volume{
				Volume: &api.VolumeAssignment{
					ID: v.ID,
				},
			},
		}
		if a.releaseDependency(mapKey, assignment, t.ID) {
			modified = true
		}
		v := store.GetVolume(readTx, v.ID)
		if v != nil {
			for _, secret := range v.Spec.Secrets {
				secretMapKey := typeAndID{objType: api.ResourceType_SECRET, id: secret.Secret}
				sa := &api.Assignment{
					Item: &api.Assignment_Secret{
						Secret: &api.Secret{ID: secret.Secret},
					},
				}
				// NOTE(dperny): it SHOULD be impossible for a modification to
				// occur due to releasing a secret when one hasn't already
				// occurred due to releasing the volume, but we'll play it
				// safe.
				if a.releaseDependency(secretMapKey, sa, t.ID) {
					modified = true
				}
			}
		}
	}

	container := t.Spec.GetContainer()

	var secrets []*api.SecretReference
	if container != nil {
		secrets = container.Secrets
	}

	for _, secretRef := range secrets {
		secretID := secretRef.SecretID
		mapKey := typeAndID{objType: api.ResourceType_SECRET, id: secretID}
		assignment := &api.Assignment{
			Item: &api.Assignment_Secret{
				Secret: &api.Secret{ID: secretID},
			},
		}
		if a.releaseDependency(mapKey, assignment, t.ID) {
			modified = true
		}
	}

	var configs []*api.ConfigReference
	if container != nil {
		configs = container.Configs
	}

	for _, configRef := range configs {
		configID := configRef.ConfigID
		mapKey := typeAndID{objType: api.ResourceType_CONFIG, id: configID}
		assignment := &api.Assignment{
			Item: &api.Assignment_Config{
				Config: &api.Config{ID: configID},
			},
		}
		if a.releaseDependency(mapKey, assignment, t.ID) {
			modified = true
		}
	}

	return modified
}

func (a *assignmentSet) addOrUpdateTask(readTx store.ReadTx, t *api.Task) bool {
	// We only care about tasks that are ASSIGNED or higher.
	if t.Status.State < api.TaskStateAssigned {
		return false
	}

	if oldTask, exists := a.tasksMap[t.ID]; exists {
		// States ASSIGNED and below are set by the orchestrator/scheduler,
		// not the agent, so tasks in these states need to be sent to the
		// agent even if nothing else has changed.
		if equality.TasksEqualStable(oldTask, t) && t.Status.State > api.TaskStateAssigned {
			// this update should not trigger a task change for the agent
			a.tasksMap[t.ID] = t
			// If this task got updated to a final state, let's release
			// the dependencies that are being used by the task
			if t.Status.State > api.TaskStateRunning {
				// If releasing the dependencies caused us to
				// remove something from the assignment set,
				// mark one modification.
				return a.releaseTaskDependencies(readTx, t)
			}
			return false
		}
	} else if t.Status.State <= api.TaskStateRunning {
		// If this task wasn't part of the assignment set before, and it's <= RUNNING
		// add the dependencies it references to the assignment.
		// Task states > RUNNING are worker reported only, are never created in
		// a > RUNNING state.
		a.addTaskDependencies(readTx, t)
	}
	a.tasksMap[t.ID] = t
	a.changes[typeAndID{objType: api.ResourceType_TASK, id: t.ID}] = &api.AssignmentChange{
		Assignment: &api.Assignment{
			Item: &api.Assignment_Task{
				Task: t,
			},
		},
		Action: api.AssignmentChange_AssignmentActionUpdate,
	}
	return true
}

// sendVolume creates a VolumeAssignment for the Volume with the given id,
// using the provided Status.
//
// returns true if a new assignment was created.
func (a *assignmentSet) sendVolume(id string, status *api.VolumePublishStatus) bool {
	// if the volume isn't pending, we have no need to worry about it.
	v, ok := a.pendingVolumes[id]
	if !ok {
		return false
	}

	// the volume is no longer pending.
	delete(a.pendingVolumes, id)

	assignment := &api.VolumeAssignment{
		ID:            v.ID,
		VolumeID:      v.VolumeInfo.VolumeID,
		Driver:        v.Spec.Driver,
		VolumeContext: v.VolumeInfo.VolumeContext,
		// TODO(dperny): PublishContext needs to be stored and retrieved. for
		// now we'll just skip it.
		PublishContext: status.PublishContext,
		// TODO(dperny): sort out AccessMode.
		Secrets: v.Spec.Secrets,
	}

	// if we're pending this volume, then create the VolumeAssignment,
	a.changes[typeAndID{objType: api.ResourceType_VOLUME, id: v.ID}] = &api.AssignmentChange{
		Assignment: &api.Assignment{
			Item: &api.Assignment_Volume{
				Volume: assignment,
			},
		},
		Action: api.AssignmentChange_AssignmentActionUpdate,
	}

	return true
}

func (a *assignmentSet) removeTask(readTx store.ReadTx, t *api.Task) bool {
	if _, exists := a.tasksMap[t.ID]; !exists {
		return false
	}

	a.changes[typeAndID{objType: api.ResourceType_TASK, id: t.ID}] = &api.AssignmentChange{
		Assignment: &api.Assignment{
			Item: &api.Assignment_Task{
				Task: &api.Task{ID: t.ID},
			},
		},
		Action: api.AssignmentChange_AssignmentActionRemove,
	}

	delete(a.tasksMap, t.ID)

	// Release the dependencies being used by this task.
	// Ignoring the return here. We will always mark this as a
	// modification, since a task is being removed.
	a.releaseTaskDependencies(readTx, t)
	return true
}

func (a *assignmentSet) message() api.AssignmentsMessage {
	var message api.AssignmentsMessage
	for _, change := range a.changes {
		message.Changes = append(message.Changes, change)
	}

	// The the set of changes is reinitialized to prepare for formation
	// of the next message.
	a.changes = make(map[typeAndID]*api.AssignmentChange)

	return message
}

// secret populates the secret value from raft store. For external secrets, the value is populated
// from the secret driver. The function returns: a secret object; an indication of whether the value
// is to be reused across tasks; and an error if the secret is not found in the store, if the secret
// driver responds with one or if the payload does not pass validation.
func (a *assignmentSet) secret(readTx store.ReadTx, task *api.Task, secretID string) (*api.Secret, bool, error) {
	secret := store.GetSecret(readTx, secretID)
	if secret == nil {
		return nil, false, fmt.Errorf("secret not found")
	}
	if secret.Spec.Driver == nil {
		return secret, false, nil
	}
	d, err := a.dp.NewSecretDriver(secret.Spec.Driver)
	if err != nil {
		return nil, false, err
	}
	value, doNotReuse, err := d.Get(&secret.Spec, task)
	if err != nil {
		return nil, false, err
	}
	if err := validation.ValidateSecretPayload(value); err != nil {
		return nil, false, err
	}
	// Assign the secret
	secret.Spec.Data = value
	return secret, doNotReuse, nil
}
