package main

import (
	"context"
	"fmt"
	"io"
	"math"
	"net"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/moby/swarmkit/v2/log"
)

// Collector waits for tasks to phone home while collecting statistics.
type Collector struct {
	mu        sync.Mutex
	start     time.Time
	durations []time.Duration
	ln        net.Listener
}

// Listen starts listening on a TCP port. Tasks have to connect to this address
// once they come online.
func (c *Collector) Listen(port int) error {
	var err error
	c.ln, err = net.Listen("tcp", ":"+strconv.Itoa(port))
	return err
}

// Collect blocks until `count` tasks phoned home.
func (c *Collector) Collect(ctx context.Context, count uint64) {
	start := time.Now()

	c.mu.Lock()
	c.start = start
	capacity := 0
	if count <= uint64(^uint(0)>>1) {
		capacity = int(count)
	}
	c.durations = make([]time.Duration, 0, capacity)
	c.mu.Unlock()

	for collected := uint64(0); collected < count; {
		conn, err := c.ln.Accept()
		if err != nil {
			log.G(ctx).WithError(err).Error("failure accepting connection")
			continue
		}
		c.mu.Lock()
		c.durations = append(c.durations, time.Since(start))
		c.mu.Unlock()
		collected++
		_ = conn.Close()
	}
}

// Stats prints various statistics related to the collection.
func (c *Collector) Stats(w io.Writer, unit time.Duration) {
	c.mu.Lock()
	values := slices.Clone(c.durations)
	start := c.start
	c.mu.Unlock()

	slices.Sort(values)

	fmt.Fprintln(w, "stats:")
	fmt.Fprintf(w, "  count:       %9d\n", len(values))

	if len(values) == 0 {
		return
	}

	du := float64(unit)
	duSuffix := unit.String()[1:]

	var sum float64
	for _, value := range values {
		sum += float64(value)
	}
	mean := sum / float64(len(values))

	var variance float64
	for _, value := range values {
		delta := float64(value) - mean
		variance += delta * delta
	}
	stddev := math.Sqrt(variance / float64(len(values)))
	fmt.Fprintf(w, "  min:         %12.2f%s\n", float64(values[0])/du, duSuffix)
	fmt.Fprintf(w, "  max:         %12.2f%s\n", float64(values[len(values)-1])/du, duSuffix)
	fmt.Fprintf(w, "  mean:        %12.2f%s\n", mean/du, duSuffix)
	fmt.Fprintf(w, "  stddev:      %12.2f%s\n", stddev/du, duSuffix)
	fmt.Fprintf(w, "  median:      %12.2f%s\n", percentile(values, 0.5)/du, duSuffix)
	fmt.Fprintf(w, "  75%%:         %12.2f%s\n", percentile(values, 0.75)/du, duSuffix)
	fmt.Fprintf(w, "  95%%:         %12.2f%s\n", percentile(values, 0.95)/du, duSuffix)
	fmt.Fprintf(w, "  99%%:         %12.2f%s\n", percentile(values, 0.99)/du, duSuffix)
	fmt.Fprintf(w, "  99.9%%:       %12.2f%s\n", percentile(values, 0.999)/du, duSuffix)
	fmt.Fprintf(w, "  rate:        %12.2f tasks/s\n", float64(len(values))/time.Since(start).Seconds())
}

func percentile(sorted []time.Duration, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}

	pos := p * float64(len(sorted)+1)
	if pos < 1 {
		return float64(sorted[0])
	}
	if pos >= float64(len(sorted)) {
		return float64(sorted[len(sorted)-1])
	}

	lower := float64(sorted[int(pos)-1])
	upper := float64(sorted[int(pos)])
	return lower + (pos-math.Floor(pos))*(upper-lower)
}
