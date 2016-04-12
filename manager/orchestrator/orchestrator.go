package orchestrator

import (
	log "github.com/Sirupsen/logrus"
	"github.com/docker/swarm-v2/identity"
	"github.com/docker/swarm-v2/manager/state"
	objectspb "github.com/docker/swarm-v2/pb/docker/cluster/objects"
	typespb "github.com/docker/swarm-v2/pb/docker/cluster/types"
)

// An Orchestrator runs a reconciliation loop to create and destroy
// tasks as necessary for the running jobs.
type Orchestrator struct {
	store state.WatchableStore

	// stopChan signals to the state machine to stop running.
	stopChan chan struct{}
	// doneChan is closed when the state machine terminates.
	doneChan chan struct{}
}

// New creates a new orchestrator.
func New(store state.WatchableStore) *Orchestrator {
	return &Orchestrator{
		store:    store,
		stopChan: make(chan struct{}),
		doneChan: make(chan struct{}),
	}
}

// Run contains the orchestrator event loop. It runs until Stop is called.
func (o *Orchestrator) Run() error {
	defer close(o.doneChan)

	// Watch changes to jobs and tasks
	queue := o.store.WatchQueue()
	watcher, cancel := queue.Watch()
	defer cancel()

	// Balance existing jobs
	var existingJobs []*objectspb.Job
	err := o.store.View(func(readTx state.ReadTx) error {
		var err error
		existingJobs, err = readTx.Jobs().Find(state.All)
		return err
	})
	if err != nil {
		return err
	}

	for _, j := range existingJobs {
		o.balance(j)
	}

	for {
		select {
		case event := <-watcher:
			switch v := event.(type) {
			case state.EventDeleteJob:
				o.deleteJob(v.Job)
			case state.EventCreateJob:
				o.balance(v.Job)
			case state.EventUpdateJob:
				o.balance(v.Job)
			case state.EventDeleteTask:
				o.deleteTask(v.Task)
			}
		case <-o.stopChan:
			return nil
		}
	}
}

// Stop stops the orchestrator.
func (o *Orchestrator) Stop() {
	close(o.stopChan)
	<-o.doneChan
}

func (o *Orchestrator) deleteJob(job *objectspb.Job) {
	log.Debugf("Job %s was deleted", job.ID)
	err := o.store.Update(func(tx state.Tx) error {
		tasks, err := tx.Tasks().Find(state.ByJobID(job.ID))
		if err != nil {
			log.Errorf("Error finding tasks for job: %v", err)
			return err
		}
		for _, t := range tasks {
			// TODO(aluzzardi): Find a better way to deal with errors.
			// If `Delete` fails, it probably means the task was already deleted which is fine.
			_ = tx.Tasks().Delete(t.ID)
		}
		return nil
	})
	if err != nil {
		log.Errorf("Error in transaction: %v", err)
	}
}

func (o *Orchestrator) deleteTask(task *objectspb.Task) {
	if task.JobID != "" {
		var job *objectspb.Job
		err := o.store.View(func(tx state.ReadTx) error {
			job = tx.Jobs().Get(task.JobID)
			return nil
		})
		if err != nil {
			log.Errorf("Error in transaction: %v", err)
		}
		if job != nil {
			o.balance(job)
		}
	}
}

func (o *Orchestrator) balance(job *objectspb.Job) {
	err := o.store.Update(func(tx state.Tx) error {
		tasks, err := tx.Tasks().Find(state.ByJobID(job.ID))
		if err != nil {
			log.Errorf("Error finding tasks for job: %v", err)
			return nil
		}

		numTasks := int64(len(tasks))

		service := job.Spec.GetService()
		// TODO(aaronl): support other types of jobs
		if service == nil {
			panic("job type not supported")
		}

		specifiedInstances := service.Instances

		switch {
		case specifiedInstances > numTasks:
			// Scale up
			log.Debugf("Job %s was scaled up from %d to %d instances", job.ID, numTasks, specifiedInstances)
			diff := specifiedInstances - numTasks
			spec := *job.Spec.Template
			meta := job.Spec.Meta // TODO(stevvooe): Copy metadata with nice name.

			for i := int64(0); i < diff; i++ {
				task := &objectspb.Task{
					ID:    identity.NewID(),
					Meta:  meta,
					Spec:  &spec,
					JobID: job.ID,
					Status: &typespb.TaskStatus{
						State: typespb.TaskStateNew,
					},
				}
				if err := tx.Tasks().Create(task); err != nil {
					log.Error(err)
				}
			}
		case specifiedInstances < numTasks:
			// Scale down
			// TODO(aaronl): Scaling down needs to involve the
			// planner. The orchestrator should not be deleting
			// tasks directly.
			log.Debugf("Job %s was scaled down from %d to %d instances", job.ID, numTasks, specifiedInstances)
			diff := numTasks - specifiedInstances
			for i := int64(0); i < diff; i++ {
				// TODO(aluzzardi): Find a better way to deal with errors.
				// If `Delete` fails, it probably means the task was already deleted which is fine.
				_ = tx.Tasks().Delete(tasks[i].ID)
			}
		}
		return nil
	})
	if err != nil {
		log.Errorf("Error in transaction: %v", err)
		return
	}
}
