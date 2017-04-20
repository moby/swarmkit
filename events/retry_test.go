package events

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestRetryingSink(t *testing.T) {
	const nevents = 100
	ts := newTestSink(t, nevents)

	// Make a sync that fails most of the time, ensuring that all the events
	// make it through.
	flaky := &flakySink{
		rate: 1.0, // start out always failing.
		Sink: ts,
	}

	breaker := NewBreaker(3, 10*time.Millisecond)
	s := NewRetryingSink(flaky, breaker)

	var wg sync.WaitGroup
	for i := 1; i <= nevents; i++ {
		event := "myevent-" + fmt.Sprint(i)

		// Above 50, set the failure rate lower
		if i > 50 {
			flaky.mu.Lock()
			flaky.rate = 0.90
			flaky.mu.Unlock()
		}

		wg.Add(1)
		go func(event Event) {
			defer wg.Done()
			if err := s.Write(event); err != nil {
				t.Fatalf("error writing event: %v", err)
			}
		}(event)
	}

	wg.Wait()
	checkClose(t, s)

	ts.mu.Lock()
	defer ts.mu.Unlock()

}
