package allocator

import (
	"sync"

	"github.com/docker/go-events"
	"github.com/docker/swarm-v2/manager/state"
	"golang.org/x/net/context"
)

// Allocator controls how the allocation stage in the manager is handled.
type Allocator struct {
	// The waitgroup on which we wait during a Stop for all go
	// routines to exit.
	wg *sync.WaitGroup

	// The cancel function that we trigger during Stop to indicate
	// all go routines to cancel themselves, cleanup and exit.
	cancel context.CancelFunc

	// The manager store.
	store state.WatchableStore

	// the ballot used to synchronize across all allocators to ensure
	// all of them have completed their respective allocations so that the
	// task can be moved to ALLOCATED state.
	taskBallot *taskBallot

	// context for the network allocator that will be needed by
	// network allocator.
	netCtx *networkContext
}

// taskBallot controls how the voting for task allocation is
// coordinated b/w different allocators. This the only structure that
// will be written by all allocator goroutines concurrently. Hence the
// mutex.
type taskBallot struct {
	sync.Mutex

	// List of registered voters who have to cast their vote to
	// indicate their allocation complete
	voters []string

	// List of votes collected for every task so far from different voters.
	votes map[string][]string
}

// allocActor controls the various phases in the lifecycle of one kind of allocator.
type allocActor struct {
	// Channel through which the allocator gets all the events
	// that it is interested in.
	ch chan events.Event

	// cancel unregisters the watcher.
	cancel func()

	// Task voter identity of the allocator.
	taskVoter string

	// Action routine which is called for every event that the
	// allocator received.
	action func(context.Context, events.Event)

	// Init routine which is called during the initialization of
	// the allocator.
	init func(ctx context.Context) error
}

// New returns a new instance of Allocator for use during allocation
// stage of the manager.
func New(store state.WatchableStore) (*Allocator, error) {
	a := &Allocator{
		store: store,
		wg:    &sync.WaitGroup{},
		taskBallot: &taskBallot{
			votes: make(map[string][]string),
		},
	}

	return a, nil
}

// Start starts all allocator go-routines which can be cancelled by invoking a subsequent Stop.
func (a *Allocator) Start(ctx context.Context) error {
	// Setup cancel context for all goroutines to use.
	ctx, cancel := context.WithCancel(ctx)
	a.cancel = cancel

	var actors []func() error
	watch, watchCancel := state.Watch(a.store.WatchQueue(),
		state.EventCreateNetwork{},
		state.EventDeleteNetwork{},
		state.EventCreateService{},
		state.EventUpdateService{},
		state.EventDeleteService{},
		state.EventCreateTask{},
		state.EventUpdateTask{},
		state.EventDeleteTask{},
		state.EventCommit{},
	)

	for _, aa := range []allocActor{
		{
			ch:        watch,
			cancel:    watchCancel,
			taskVoter: networkVoter,
			init:      a.doNetworkInit,
			action:    a.doNetworkAlloc,
		},
	} {
		if aa.taskVoter != "" {
			a.registerToVote(aa.taskVoter)
		}

		// Copy the iterated value for variable capture.
		aaCopy := aa
		actor := func() error {
			// init might return an allocator specific context
			// which is a child of the passed in context to hold
			// allocator specific state
			if err := aaCopy.init(ctx); err != nil {
				// Stop the watches for this allocator
				// if we are failing in the init of
				// this allocator.
				aa.cancel()
				return err
			}

			go a.run(ctx, aaCopy)
			return nil
		}

		actors = append(actors, actor)
	}

	for _, actor := range actors {
		if err := actor(); err != nil {
			// Make sure all the allocators which were
			// successfully started, are stopped before
			// bailing out.
			a.Stop()

			return err
		}
	}

	return nil
}

// Stop stops the allocator
func (a *Allocator) Stop() {
	// Cancel all allocator goroutines
	a.cancel()

	// Wait for all allocator goroutines to truly exit
	a.wg.Wait()
}

func (a *Allocator) run(ctx context.Context, aa allocActor) {
	a.wg.Add(1)
	defer a.wg.Done()
	defer a.cancel()

	for {
		select {
		case ev, ok := <-aa.ch:
			if !ok {
				return
			}

			aa.action(ctx, ev)
		case <-ctx.Done():
			return
		}
	}
}

func (a *Allocator) registerToVote(name string) {
	a.taskBallot.Lock()
	defer a.taskBallot.Unlock()

	a.taskBallot.voters = append(a.taskBallot.voters, name)
}

func (a *Allocator) taskAllocateVote(voter string, id string) bool {
	a.taskBallot.Lock()
	defer a.taskBallot.Unlock()

	a.taskBallot.votes[id] = append(a.taskBallot.votes[id], voter)

	// We haven't gotten enough votes yet
	if len(a.taskBallot.voters) > len(a.taskBallot.votes[id]) {
		return false
	}

nextVoter:
	for _, voter := range a.taskBallot.voters {
		for _, vote := range a.taskBallot.votes[id] {
			if voter == vote {
				continue nextVoter
			}
		}

		// Not every registered voter has registered a vote.
		return false
	}

	return true
}
