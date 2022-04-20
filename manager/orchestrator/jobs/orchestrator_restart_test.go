package jobs

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"github.com/docker/go-events"
	gogotypes "github.com/gogo/protobuf/types"

	"github.com/moby/swarmkit/v2/api"
	"github.com/moby/swarmkit/v2/manager/state/store"
)

// passEventsUntil is a helper method that calls handleEvent on all events from
// the orchestrator's watchChan until the provided closure returns True.
func passEventsUntil(o *Orchestrator, check func(event events.Event) bool) {
	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()
	for {
		select {
		case event := <-o.watchChan:
			// regardless of whether this is the desired event,
			// pass it down to the orchestrator to handle.
			o.handleEvent(context.Background(), event)
			if check(event) {
				return
			}
		// on the off chance that we never get an event, bail out of the
		// loop after 10 seconds.
		case <-deadline.C:
			return
		}
	}
}

var _ = Describe("Jobs RestartSupervisor Integration", func() {
	var (
		s *store.MemoryStore
		o *Orchestrator

		service *api.Service
	)

	BeforeEach(func() {
		s = store.NewMemoryStore(nil)
		Expect(s).ToNot(BeNil())

		service = &api.Service{
			ID: "norestartservice",
			Spec: api.ServiceSpec{
				Annotations: api.Annotations{
					Name: "norestartservice",
				},
				Mode: &api.ServiceSpec_ReplicatedJob{
					ReplicatedJob: &api.ReplicatedJob{
						MaxConcurrent:    uint64(1),
						TotalCompletions: uint64(1),
					},
				},
				Task: api.TaskSpec{},
			},
			JobStatus: &api.JobStatus{
				JobIteration: api.Version{
					Index: 0,
				},
			},
		}

		o = NewOrchestrator(s)
	})

	JustBeforeEach(func() {
		o.init(context.Background())
	})

	AfterEach(func() {
		o.watchCancel()
	})

	It("should not restart tasks if the Condition is RestartOnNone", func() {
		service.Spec.Task.Restart = &api.RestartPolicy{
			Condition: api.RestartOnNone,
		}
		err := s.Update(func(tx store.Tx) error {
			return store.CreateService(tx, service)
		})
		Expect(err).ToNot(HaveOccurred())

		// read out from the events channel until we send down the
		// EventCreateService for the service we just created. By doing this
		// manually and in a blocking fashion like this, we avoid relying on
		// the go scheduler to pick up this routine.
		passEventsUntil(o, serviceCreated(service))

		// now, after having handled the creation event, we should have created
		// a Task
		found := false
		s.View(func(tx store.ReadTx) {
			var tasks []*api.Task
			tasks, err = store.FindTasks(tx, store.ByServiceID(service.ID))
			if err != nil {
				return
			}

			// the task is found if it exists, its job iteration is 0, its
			// state is New, and it's DesiredState is Completed.
			found = len(tasks) == 1 &&
				tasks[0].JobIteration != nil &&
				tasks[0].JobIteration.Index == 0 &&
				tasks[0].Status.State == api.TaskStateNew &&
				tasks[0].DesiredState == api.TaskStateCompleted
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(found).To(BeTrue())

		// now, fail the task
		err = s.Update(func(tx store.Tx) error {
			tasks, err := store.FindTasks(tx, store.ByServiceID(service.ID))
			if err != nil {
				return err
			}
			if len(tasks) != 1 {
				return fmt.Errorf("there should only be one task at this stage, but there are %v", len(tasks))
			}
			tasks[0].Status.State = api.TaskStateFailed
			return store.UpdateTask(tx, tasks[0])
		})
		Expect(err).ToNot(HaveOccurred())

		// pass events again, this time until the TaskUpdate event with the
		// failed state goes through
		passEventsUntil(o, func(event events.Event) bool {
			updateEv, ok := event.(api.EventUpdateTask)
			return ok && updateEv.Task.Status.State == api.TaskStateFailed
		})

		// now, having processed that event, we should verify that no new Task
		// has been created.
		count := 0
		s.View(func(tx store.ReadTx) {
			var tasks []*api.Task
			tasks, err = store.FindTasks(tx, store.ByServiceID(service.ID))
			count = len(tasks)
		})
		Expect(count).To(Equal(1))
	})

	It("should only restart tasks MaxAttempt times", func() {
		service.Spec.Task.Restart = &api.RestartPolicy{
			Condition:   api.RestartOnFailure,
			MaxAttempts: 3,
			// set a low but non-zero delay duration, so we avoid default
			// duration, which may be long.
			Delay: gogotypes.DurationProto(100 * time.Millisecond),
		}
		err := s.Update(func(tx store.Tx) error {
			return store.CreateService(tx, service)
		})
		Expect(err).ToNot(HaveOccurred())

		passEventsUntil(o, serviceCreated(service))

		// first, we will check that the previous iteration successfully
		// created the task. if so, we fail it. then, we wait until the task
		// has successfully fully started. this means we fail 3 tasks in total.
		// failing the fourth task happens after this loop, and is the
		// iteration that should not create a new task.
		for i := 0; i < 3; i++ {
			err = s.Update(func(tx store.Tx) error {
				tasks, err := store.FindTasks(tx, store.ByTaskState(api.TaskStateNew))
				if err != nil {
					return err
				}
				if len(tasks) < 1 {
					return fmt.Errorf("could not find new task")
				}
				if len(tasks) > 1 {
					return fmt.Errorf("too many new tasks, there are %v", len(tasks))
				}
				tasks[0].Status.State = api.TaskStateFailed
				return store.UpdateTask(tx, tasks[0])
			})
			Expect(err).ToNot(HaveOccurred())

			// first, make sure the task fail event is handled
			passEventsUntil(o, taskFailed)

			// now wait for the new task to be created, and for it to move past
			// desired state READY. the task delay means this part is async,
			// but we have a long timeout and a short delay, so the scheduler
			// shouldn't mess this up.
			passEventsUntil(o, func(event events.Event) bool {
				updated, ok := event.(api.EventUpdateTask)
				return ok &&
					updated.Task.DesiredState == api.TaskStateCompleted &&
					updated.OldTask.DesiredState == api.TaskStateReady
			})
		}

		err = s.Update(func(tx store.Tx) error {
			tasks, err := store.FindTasks(tx, store.ByTaskState(api.TaskStateNew))
			if err != nil {
				return err
			}
			if len(tasks) < 1 {
				return fmt.Errorf("could not find new task")
			}
			if len(tasks) > 1 {
				return fmt.Errorf("too many new tasks, there are %v", len(tasks))
			}
			tasks[0].Status.State = api.TaskStateFailed
			return store.UpdateTask(tx, tasks[0])
		})

		passEventsUntil(o, taskFailed)

		// now check that no new task has been created
		var tasks []*api.Task
		s.View(func(tx store.ReadTx) {
			tasks, err = store.FindTasks(tx, store.All)
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(tasks).To(HaveLen(4))

		for _, task := range tasks {
			Expect(task.Status.State).To(Equal(api.TaskStateFailed))
		}
	})
})

func serviceCreated(service *api.Service) func(events.Event) bool {
	return func(event events.Event) bool {
		create, ok := event.(api.EventCreateService)
		return ok && create.Service.ID == service.ID
	}
}

func taskFailed(event events.Event) bool {
	updated, ok := event.(api.EventUpdateTask)
	return ok &&
		updated.Task.Status.State == api.TaskStateFailed &&
		updated.OldTask.Status.State == api.TaskStateNew
}
