package orchestrator

import (
	log "github.com/Sirupsen/logrus"
	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/identity"
	"github.com/docker/swarm-v2/state"
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

// NewOrchestrator creates a new orchestrator.
func NewOrchestrator(store state.WatchableStore) *Orchestrator {
	return &Orchestrator{
		store:    store,
		stopChan: make(chan struct{}),
		doneChan: make(chan struct{}),
	}
}

// Run contains the orchestrator event loop. It runs until Stop is called.
func (o *Orchestrator) Run() {
	defer close(o.doneChan)

	// Watch changes to jobs and tasks
	queue := o.store.WatchQueue()
	watcher := queue.Watch()

	// Balance existing jobs
	var existingJobs []*api.Job
	err := o.store.View(func(readTx state.ReadTx) error {
		var err error
		existingJobs, err = readTx.Jobs().Find(state.All)
		return err
	})
	if err != nil {
		log.Errorf("error querying existing jobs: %v", err)
	}

	for _, j := range existingJobs {
		o.balance(j)
	}

	for {
		select {
		case event, ok := <-watcher:
			if !ok {
				return
			}
			switch v := event.Payload.(type) {
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
			queue.StopWatch(watcher)
		}
	}
}

// Stop stops the orchestrator.
func (o *Orchestrator) Stop() {
	close(o.stopChan)
	<-o.doneChan
}

func (o *Orchestrator) deleteJob(job *api.Job) {
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

func (o *Orchestrator) deleteTask(task *api.Task) {
	if task.JobID != "" {
		var job *api.Job
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

func (o *Orchestrator) balance(job *api.Job) {
	err := o.store.Update(func(tx state.Tx) error {
		tasks, err := tx.Tasks().Find(state.ByJobID(job.ID))
		if err != nil {
			log.Errorf("Error finding tasks for job: %v", err)
			return nil
		}

		numTasks := int64(len(tasks))

		serviceJob, ok := job.Spec.Orchestration.Job.(*api.JobSpec_Orchestration_Service)
		// TODO(aaronl): support other types of jobs
		if !ok {
			panic("job type not supported")
		}

		specifiedInstances := serviceJob.Service.Instances

		switch {
		case specifiedInstances > numTasks:
			// Scale up
			log.Debugf("Job %s was scaled up from %d to %d instances", job.ID, numTasks, specifiedInstances)
			diff := specifiedInstances - numTasks
			for i := int64(0); i < diff; i++ {
				spec := *job.Spec
				task := &api.Task{
					Spec:  &spec,
					JobID: job.ID,
					Status: &api.TaskStatus{
						State: api.TaskStatus_NEW,
					},
				}
				var err error
				task.ID, err = identity.NewID()
				if err != nil {
					log.Error(err)
				} else if err := tx.Tasks().Create(task); err != nil {
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
