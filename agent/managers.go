package agent

import (
	"math"
	"math/rand"
	"sort"
	"sync"

	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/transport"
)

// Managers keeps track of dispatcher addresses by weight, informed by
// observations.
type Managers interface {
	// Weight returns the managers with their current weights.
	Weights() map[string]int

	// Select a manager from the set of available managers.
	Select() (string, error)

	// Observe records an experience with a particular manager. A positive weight
	// indicates a good experience and a negative weight a bad experience.
	//
	// The observation will be used to calculate a moving weight, which is
	// implementation dependent. This method will be called such that repeated
	// observations of the same master in each session request are favored.
	Observe(addr string, weight int)

	// Remove the manager from the list completely.
	Remove(addrs ...string)
}

// NewManagers returns a Managers instance with the provided set of addresses.
// Entries provided are heavily weighted initially.
func NewManagers(addrs ...string) Managers {
	mwr := &managersWeightedRandom{
		managers: make(map[string]int),
	}

	for _, addr := range addrs {
		mwr.Observe(addr, 1)
	}

	return mwr
}

type managersWeightedRandom struct {
	managers map[string]int
	mu       sync.Mutex

	// workspace to avoid reallocation. these get lazily allocated when
	// selecting values.
	cdf   []float64
	addrs []string
}

func (mwr *managersWeightedRandom) Weights() map[string]int {
	mwr.mu.Lock()
	defer mwr.mu.Unlock()

	ms := make(map[string]int, len(mwr.managers))
	for addr, weight := range mwr.managers {
		ms[addr] = weight
	}

	return ms
}

func (mwr *managersWeightedRandom) Select() (string, error) {
	mwr.mu.Lock()
	defer mwr.mu.Unlock()

	if len(mwr.managers) == 0 {
		return "", errManagersUnavailable
	}

	// NOTE(stevvooe): We then use a weighted random selection algorithm
	// (http://stackoverflow.com/questions/4463561/weighted-random-selection-from-array)
	// to choose the master to connect to.
	//
	// It is possible that this is insufficient. The following may inform a
	// better solution:

	// https://github.com/LK4D4/sample
	//
	// The first link applies exponential distribution weight choice reservior
	// sampling. This may be relevant if we view the master selection as a
	// distributed reservior sampling problem.

	// bias to zero-weighted managers have same probability. otherwise, we
	// always select first entry when all are zero.
	const bias = 0.1

	// clear out workspace
	mwr.cdf = mwr.cdf[:0]
	mwr.addrs = mwr.addrs[:0]

	cum := 0.0
	// calculate CDF over weights
	for addr, weight := range mwr.managers {
		if weight < 0 {
			// treat these as zero, to keep there selection unlikely.
			weight = 0
		}

		cum += float64(weight) + bias
		mwr.cdf = append(mwr.cdf, cum)
		mwr.addrs = append(mwr.addrs, addr)
	}

	r := mwr.cdf[len(mwr.cdf)-1] * rand.Float64()
	i := sort.SearchFloat64s(mwr.cdf, r)
	return mwr.addrs[i], nil
}

func (mwr *managersWeightedRandom) Observe(addr string, weight int) {
	mwr.mu.Lock()
	defer mwr.mu.Unlock()

	mwr.observe(addr, float64(weight))
}

func (mwr *managersWeightedRandom) Remove(addrs ...string) {
	mwr.mu.Lock()
	defer mwr.mu.Unlock()

	for _, addr := range addrs {
		delete(mwr.managers, addr)
	}
}

const (
	// managerWeightSmoothingFactor for exponential smoothing. This adjusts how
	// much of the // observation and old value we are using to calculate the new value.
	// See
	// https://en.wikipedia.org/wiki/Exponential_smoothing#Basic_exponential_smoothing
	// for details.
	managerWeightSmoothingFactor = 0.7
	managerWeightMax             = 1 << 8
)

func clip(x float64) float64 {
	if math.IsNaN(x) {
		// treat garbage as such
		// acts like a no-op for us.
		return 0
	}
	return math.Max(math.Min(managerWeightMax, x), -managerWeightMax)
}

func (mwr *managersWeightedRandom) observe(addr string, weight float64) {

	// While we have a decent, ad-hoc approach here to weight subsequent
	// observerations, we may want to look into applying forward decay:
	//
	//  http://dimacs.rutgers.edu/~graham/pubs/papers/fwddecay.pdf
	//
	// We need to get better data from behavior in a cluster.

	// makes the math easier to read below
	var (
		w0 = float64(mwr.managers[addr])
		w1 = clip(weight)
	)
	const α = managerWeightSmoothingFactor

	// Multiply the new value to current value, and appy smoothing against the old
	// value.
	wn := clip(α*w1 + (1-α)*w0)

	mwr.managers[addr] = int(math.Ceil(wn))
}

type picker struct {
	m    Managers
	addr string // currently selected manager address
	conn *grpc.Conn
	mu   sync.Mutex
}

var _ grpc.Picker = &picker{}

func newPicker(initial string, m Managers) *picker {
	return &picker{m: m, addr: initial}
}

// Init does initial processing for the Picker, e.g., initiate some connections.
func (p *picker) Init(cc *grpc.ClientConn) error {
	p.mu.Lock()
	addr := p.addr
	p.mu.Unlock()

	p.m.Observe(addr, 1)
	c, err := grpc.NewConn(cc)
	if err != nil {
		return err
	}

	p.mu.Lock()
	p.conn = c
	p.mu.Unlock()
	return nil
}

// Pick blocks until either a transport.ClientTransport is ready for the upcoming RPC
// or some error happens.
func (p *picker) Pick(ctx context.Context) (transport.ClientTransport, error) {
	p.mu.Lock()
	addr := p.addr
	p.mu.Unlock()
	transport, err := p.conn.Wait(ctx)
	if err != nil {

		p.m.Observe(addr, -1)
	}

	return transport, err
}

// PickAddr picks a peer address for connecting. This will be called repeated for
// connecting/reconnecting.
func (p *picker) PickAddr() (string, error) {
	p.mu.Lock()
	addr := p.addr
	p.mu.Unlock()

	p.m.Observe(addr, -1) // downweight the current addr

	var err error
	addr, err = p.m.Select()
	if err != nil {
		return "", err
	}

	p.mu.Lock()
	p.addr = addr
	p.mu.Unlock()
	return p.addr, err
}

// State returns the connectivity state of the underlying connections.
func (p *picker) State() (grpc.ConnectivityState, error) {
	return p.conn.State(), nil
}

// WaitForStateChange blocks until the state changes to something other than
// the sourceState. It returns the new state or error.
func (p *picker) WaitForStateChange(ctx context.Context, sourceState grpc.ConnectivityState) (grpc.ConnectivityState, error) {
	p.mu.Lock()
	conn := p.conn
	addr := p.addr
	p.mu.Unlock()

	state, err := conn.WaitForStateChange(ctx, sourceState)
	if err != nil {
		return state, err
	}

	// TODO(stevvooe): We may want to actually score the transition by checking
	// sourceState.

	// TODO(stevvooe): This is questionable, but we'll see how it works.
	switch state {
	case grpc.Idle:
		p.m.Observe(addr, 1)
	case grpc.Connecting:
		p.m.Observe(addr, 1)
	case grpc.Ready:
		p.m.Observe(addr, 1)
	case grpc.TransientFailure:
		p.m.Observe(addr, -1)
	case grpc.Shutdown:
		p.m.Observe(addr, -1)
	}

	return state, err
}

// Reset the current connection and force a reconnect to another address.
func (p *picker) Reset() error {
	p.mu.Lock()
	conn := p.conn
	p.mu.Unlock()

	conn.NotifyReset()
	return nil
}

// Close closes all the Conn's owned by this Picker.
func (p *picker) Close() error {
	p.mu.Lock()
	conn := p.conn
	p.mu.Unlock()

	return conn.Close()
}
