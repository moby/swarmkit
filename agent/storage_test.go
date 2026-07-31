package agent

import (
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/moby/swarmkit/v2/api"
	"github.com/moby/swarmkit/v2/identity"
	"github.com/stretchr/testify/assert"
	bolt "go.etcd.io/bbolt"
)

func TestStorageInit(t *testing.T) {
	db, cleanup := storageTestEnv(t)
	defer cleanup()

	assert.NoError(t, InitDB(db)) // ensure idempotence.
	assert.NoError(t, db.View(func(tx *bolt.Tx) error {
		bkt := tx.Bucket(bucketKeyStorageVersion)
		assert.NotNil(t, bkt)

		tbkt := bkt.Bucket([]byte("tasks"))
		assert.NotNil(t, tbkt)

		return nil
	}))
}

func TestStoragePutGet(t *testing.T) {
	db, cleanup := storageTestEnv(t)
	defer cleanup()

	tasks := genTasks(20)

	assert.NoError(t, db.Update(func(tx *bolt.Tx) error {
		for _, task := range tasks {
			assert.NoError(t, PutTask(tx, task))
		}

		return nil
	}))

	assert.NoError(t, db.View(func(tx *bolt.Tx) error {
		for _, task := range tasks {
			retrieved, err := GetTask(tx, task.Id)
			assert.NoError(t, err)
			// PutTask stores the task with its status blanked out.
			want := task.Copy()
			want.Status = nil
			assert.True(t, want.EqualVT(retrieved), "task %s round-tripped unequal", task.Id)
		}

		return nil
	}))
}

func TestStoragePutGetStatusAssigned(t *testing.T) {
	db, cleanup := storageTestEnv(t)
	defer cleanup()

	tasks := genTasks(20)

	// set task, status and assignment for all tasks.
	assert.NoError(t, db.Update(func(tx *bolt.Tx) error {
		for _, task := range tasks {
			assert.NoError(t, PutTask(tx, task))
			assert.NoError(t, PutTaskStatus(tx, task.Id, task.Status))
			assert.NoError(t, SetTaskAssignment(tx, task.Id, true))
		}

		return nil
	}))

	assert.NoError(t, db.View(func(tx *bolt.Tx) error {
		for _, task := range tasks {
			status, err := GetTaskStatus(tx, task.Id)
			assert.NoError(t, err)
			assert.True(t, task.Status.EqualVT(status), "status for %s round-tripped unequal", task.Id)

			retrieved, err := GetTask(tx, task.Id)
			assert.NoError(t, err)

			// PutTask stores the task with its status blanked out. Compare
			// against a copy: the status is still needed below.
			want := task.Copy()
			want.Status = nil
			assert.True(t, want.EqualVT(retrieved), "task %s round-tripped unequal", task.Id)

			assert.True(t, TaskAssigned(tx, task.Id))
		}

		return nil
	}))

	// set evens to unassigned and updates all states plus one
	assert.NoError(t, db.Update(func(tx *bolt.Tx) error {
		for i, task := range tasks {
			task.Status.State++
			assert.NoError(t, PutTaskStatus(tx, task.Id, task.Status))

			if i%2 == 0 {
				assert.NoError(t, SetTaskAssignment(tx, task.Id, false))
			}
		}

		return nil
	}))

	assert.NoError(t, db.View(func(tx *bolt.Tx) error {
		for i, task := range tasks {
			status, err := GetTaskStatus(tx, task.Id)
			assert.NoError(t, err)
			assert.True(t, task.Status.EqualVT(status), "status for %s round-tripped unequal", task.Id)

			retrieved, err := GetTask(tx, task.Id)
			assert.NoError(t, err)

			// PutTask stores the task with its status blanked out. Compare
			// against a copy: the status is still needed below.
			want := task.Copy()
			want.Status = nil
			assert.True(t, want.EqualVT(retrieved), "task %s round-tripped unequal", task.Id)

			if i%2 == 0 {
				assert.False(t, TaskAssigned(tx, task.Id))
			} else {
				assert.True(t, TaskAssigned(tx, task.Id))
			}

		}

		return nil
	}))
}

func genTasks(n int) []*api.Task {
	var tasks []*api.Task
	for range n {
		tasks = append(tasks, genTask())
	}

	sort.Stable(tasksByID(tasks))

	return tasks
}

func genTask() *api.Task {
	return &api.Task{
		Id:        identity.NewID(),
		ServiceId: identity.NewID(),
		Status:    genTaskStatus(),
		Spec: &api.TaskSpec{
			Runtime: &api.TaskSpec_Container{
				Container: &api.ContainerSpec{
					Image:   "foo",
					Command: []string{"this", "-w", "works"},
				},
			},
		},
	}
}

var taskStates = []api.TaskState{
	api.TaskState_ASSIGNED, api.TaskState_ACCEPTED,
	api.TaskState_PREPARING, api.TaskState_READY,
	api.TaskState_STARTING, api.TaskState_RUNNING,
	api.TaskState_COMPLETE, api.TaskState_FAILED,
	api.TaskState_REJECTED, api.TaskState_SHUTDOWN,
}

func genTaskStatus() *api.TaskStatus {
	return &api.TaskStatus{
		State:   taskStates[rand.Intn(len(taskStates))],
		Message: identity.NewID(), // just put some garbage here.
	}
}

// storageTestEnv returns an initialized db and cleanup function for use in
// tests.
func storageTestEnv(t *testing.T) (*bolt.DB, func()) {
	t.Helper()
	var cleanup []func()
	dir := t.TempDir()
	dbpath := filepath.Join(dir, "tasks.db")
	assert.NoError(t, os.MkdirAll(dir, 0o777))

	db, err := bolt.Open(dbpath, 0666, nil)
	assert.NoError(t, err)
	cleanup = append(cleanup, func() { db.Close() })

	assert.NoError(t, InitDB(db))
	return db, func() {
		// iterate in reverse so it works like defer
		for i := len(cleanup) - 1; i >= 0; i-- {
			cleanup[i]()
		}
	}
}

type tasksByID []*api.Task

func (ts tasksByID) Len() int           { return len(ts) }
func (ts tasksByID) Less(i, j int) bool { return ts[i].Id < ts[j].Id }
func (ts tasksByID) Swap(i, j int)      { ts[i], ts[j] = ts[j], ts[i] }
