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
	store state.Store

	// stopChan signals to the state machine to stop running.
	stopChan chan struct{}
	// doneChan is closed when the state machine terminates.
	doneChan chan struct{}
}

// NewOrchestrator creates a new orchestrator.
func NewOrchestrator(store state.Store) *Orchestrator {
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

	for {
		select {
		case event, ok := <-watcher:
			if !ok {
				return
			}
			switch v := event.Payload.(type) {
			case state.EventDeleteJob:
				log.Debugf("Job %s was deleted", v.Job.ID)
				for _, t := range o.store.TasksByJob(v.Job.ID) {
					o.store.DeleteTask(t.ID)
				}
			case state.EventCreateJob:
				o.balance(v.Job)
			case state.EventUpdateJob:
				o.balance(v.Job)
			case state.EventDeleteTask:
				if v.Task.JobID != "" {
					job := o.store.Job(v.Task.JobID)
					if job != nil {
						o.balance(job)
					}
				}
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

func (o *Orchestrator) balance(job *api.Job) {
	tasks := o.store.TasksByJob(job.ID)
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
			} else if err := o.store.CreateTask(task.ID, task); err != nil {
				log.Error(err)
			}
		}
	case specifiedInstances < numTasks:
		// Scale down
		log.Debugf("Job %s was scaled down from %d to %d instances", job.ID, numTasks, specifiedInstances)
		diff := numTasks - specifiedInstances
		for i := int64(0); i < diff; i++ {
			o.store.DeleteTask(tasks[i].ID)
		}
	}
}
