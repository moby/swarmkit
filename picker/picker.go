package picker

import (
	"fmt"
	"math"
	"math/rand"
	"sort"
	"sync"

	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/transport"
)

var errRemotesUnavailable = fmt.Errorf("no remote hosts provided")

// Remotes keeps track of remote addresses by weight, informed by
// observations.
type Remotes interface {
	// Weight returns the remotes with their current weights.
	Weights() map[string]int

	// Select a remote from the set of available remotes.
	Select() (string, error)

	// Observe records an experience with a particular remote. A positive weight
	// indicates a good experience and a negative weight a bad experience.
	//
	// The observation will be used to calculate a moving weight, which is
	// implementation dependent. This method will be called such that repeated
	// observations of the same master in each session request are favored.
	Observe(addr string, weight int)

	// Remove the remote from the list completely.
	Remove(addrs ...string)
}

// NewRemotes returns a Remotes instance with the provided set of addresses.
// Entries provided are heavily weighted initially.
func NewRemotes(addrs ...string) Remotes {
	mwr := &remotesWeightedRandom{
		remotes: make(map[string]int),
	}

	for _, addr := range addrs {
		mwr.Observe(addr, 1)
	}

	return mwr
}

type remotesWeightedRandom struct {
	remotes map[string]int
	mu      sync.Mutex

	// workspace to avoid reallocation. these get lazily allocated when
	// selecting values.
	cdf   []float64
	addrs []string
}

func (mwr *remotesWeightedRandom) Weights() map[string]int {
	mwr.mu.Lock()
	defer mwr.mu.Unlock()

	ms := make(map[string]int, len(mwr.remotes))
	for addr, weight := range mwr.remotes {
		ms[addr] = weight
	}

	return ms
}

func (mwr *remotesWeightedRandom) Select() (string, error) {
	mwr.mu.Lock()
	defer mwr.mu.Unlock()

	if len(mwr.remotes) == 0 {
		return "", errRemotesUnavailable
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

	// bias to zero-weighted remotes have same probability. otherwise, we
	// always select first entry when all are zero.
	const bias = 0.1

	// clear out workspace
	mwr.cdf = mwr.cdf[:0]
	mwr.addrs = mwr.addrs[:0]

	cum := 0.0
	// calculate CDF over weights
	for addr, weight := range mwr.remotes {
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

func (mwr *remotesWeightedRandom) Observe(addr string, weight int) {
	mwr.mu.Lock()
	defer mwr.mu.Unlock()

	mwr.observe(addr, float64(weight))
}

func (mwr *remotesWeightedRandom) Remove(addrs ...string) {
	mwr.mu.Lock()
	defer mwr.mu.Unlock()

	for _, addr := range addrs {
		delete(mwr.remotes, addr)
	}
}

const (
	// remoteWeightSmoothingFactor for exponential smoothing. This adjusts how
	// much of the // observation and old value we are using to calculate the new value.
	// See
	// https://en.wikipedia.org/wiki/Exponential_smoothing#Basic_exponential_smoothing
	// for details.
	remoteWeightSmoothingFactor = 0.7
	remoteWeightMax             = 1 << 8
)

func clip(x float64) float64 {
	if math.IsNaN(x) {
		// treat garbage as such
		// acts like a no-op for us.
		return 0
	}
	return math.Max(math.Min(remoteWeightMax, x), -remoteWeightMax)
}

func (mwr *remotesWeightedRandom) observe(addr string, weight float64) {

	// While we have a decent, ad-hoc approach here to weight subsequent
	// observerations, we may want to look into applying forward decay:
	//
	//  http://dimacs.rutgers.edu/~graham/pubs/papers/fwddecay.pdf
	//
	// We need to get better data from behavior in a cluster.

	// makes the math easier to read below
	var (
		w0 = float64(mwr.remotes[addr])
		w1 = clip(weight)
	)
	const α = remoteWeightSmoothingFactor

	// Multiply the new value to current value, and appy smoothing against the old
	// value.
	wn := clip(α*w1 + (1-α)*w0)

	mwr.remotes[addr] = int(math.Ceil(wn))
}

// Picker implements a grpc Picker
type Picker struct {
	r    Remotes
	addr string // currently selected remote address
	conn *grpc.Conn
	mu   sync.Mutex
}

var _ grpc.Picker = &Picker{}

// NewPicker returns a Picker
func NewPicker(initial string, r Remotes) *Picker {
	return &Picker{r: r, addr: initial}
}

// Init does initial processing for the Picker, e.g., initiate some connections.
func (p *Picker) Init(cc *grpc.ClientConn) error {
	p.mu.Lock()
	addr := p.addr
	p.mu.Unlock()

	p.r.Observe(addr, 1)
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
func (p *Picker) Pick(ctx context.Context) (transport.ClientTransport, error) {
	p.mu.Lock()
	addr := p.addr
	p.mu.Unlock()
	transport, err := p.conn.Wait(ctx)
	if err != nil {

		p.r.Observe(addr, -1)
	}

	return transport, err
}

// PickAddr picks a peer address for connecting. This will be called repeated for
// connecting/reconnecting.
func (p *Picker) PickAddr() (string, error) {
	p.mu.Lock()
	addr := p.addr
	p.mu.Unlock()

	p.r.Observe(addr, -1) // downweight the current addr

	var err error
	addr, err = p.r.Select()
	if err != nil {
		return "", err
	}

	p.mu.Lock()
	p.addr = addr
	p.mu.Unlock()
	return p.addr, err
}

// State returns the connectivity state of the underlying connections.
func (p *Picker) State() (grpc.ConnectivityState, error) {
	return p.conn.State(), nil
}

// WaitForStateChange blocks until the state changes to something other than
// the sourceState. It returns the new state or error.
func (p *Picker) WaitForStateChange(ctx context.Context, sourceState grpc.ConnectivityState) (grpc.ConnectivityState, error) {
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
		p.r.Observe(addr, 1)
	case grpc.Connecting:
		p.r.Observe(addr, 1)
	case grpc.Ready:
		p.r.Observe(addr, 1)
	case grpc.TransientFailure:
		p.r.Observe(addr, -1)
	case grpc.Shutdown:
		p.r.Observe(addr, -1)
	}

	return state, err
}

// Reset the current connection and force a reconnect to another address.
func (p *Picker) Reset() error {
	p.mu.Lock()
	conn := p.conn
	p.mu.Unlock()

	conn.NotifyReset()
	return nil
}

// Close closes all the Conn's owned by this Picker.
func (p *Picker) Close() error {
	p.mu.Lock()
	conn := p.conn
	p.mu.Unlock()

	return conn.Close()
}
