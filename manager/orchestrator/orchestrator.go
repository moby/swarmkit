package orchestrator

import (
	log "github.com/Sirupsen/logrus"
	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/identity"
	"github.com/docker/swarm-v2/manager/state"
)

// An Orchestrator runs a reconciliation loop to create and destroy
// tasks as necessary for the running services.
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

	// Watch changes to services and tasks
	queue := o.store.WatchQueue()
	watcher, cancel := queue.Watch()
	defer cancel()

	// Balance existing services
	var existingServices []*api.Service
	err := o.store.View(func(readTx state.ReadTx) error {
		var err error
		existingServices, err = readTx.Services().Find(state.All)
		return err
	})
	if err != nil {
		return err
	}

	for _, j := range existingServices {
		o.balance(j)
	}

	for {
		select {
		case event := <-watcher:
			switch v := event.(type) {
			case state.EventDeleteService:
				o.deleteService(v.Service)
			case state.EventCreateService:
				o.balance(v.Service)
			case state.EventUpdateService:
				o.balance(v.Service)
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

func (o *Orchestrator) deleteService(service *api.Service) {
	log.Debugf("Service %s was deleted", service.ID)
	err := o.store.Update(func(tx state.Tx) error {
		tasks, err := tx.Tasks().Find(state.ByServiceID(service.ID))
		if err != nil {
			log.Errorf("Error finding tasks for service: %v", err)
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
	if task.ServiceID != "" {
		var service *api.Service
		err := o.store.View(func(tx state.ReadTx) error {
			service = tx.Services().Get(task.ServiceID)
			return nil
		})
		if err != nil {
			log.Errorf("Error in transaction: %v", err)
		}
		if service != nil {
			o.balance(service)
		}
	}
}

func (o *Orchestrator) balance(service *api.Service) {
	err := o.store.Update(func(tx state.Tx) error {
		tasks, err := tx.Tasks().Find(state.ByServiceID(service.ID))
		if err != nil {
			log.Errorf("Error finding tasks for service: %v", err)
			return nil
		}

		numTasks := int64(len(tasks))

		spec := service.Spec
		specifiedInstances := spec.Instances

		switch {
		case specifiedInstances > numTasks:
			// Scale up
			log.Debugf("Service %s was scaled up from %d to %d instances", service.ID, numTasks, specifiedInstances)
			diff := specifiedInstances - numTasks
			spec := *service.Spec.Template
			meta := service.Spec.Meta // TODO(stevvooe): Copy metadata with nice name.

			for i := int64(0); i < diff; i++ {
				task := &api.Task{
					ID:        identity.NewID(),
					Meta:      meta,
					Spec:      &spec,
					ServiceID: service.ID,
					Status: &api.TaskStatus{
						State: api.TaskStateNew,
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
			log.Debugf("Service %s was scaled down from %d to %d instances", service.ID, numTasks, specifiedInstances)
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
