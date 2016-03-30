package watch

import (
	"sync"
	"testing"

	"github.com/docker/go-events"
)

func TestWatch(t *testing.T) {
	// Create a queue
	q := NewQueue(0)

	type testEvent struct {
		tags []string
		str  string
	}

	tagFilter := func(t string) events.Matcher {
		return events.MatcherFunc(func(event events.Event) bool {
			testEvent := event.(testEvent)
			for _, itemTag := range testEvent.tags {
				if t == itemTag {
					return true
				}
			}
			return false
		})
	}

	// Create filtered watchers
	c1, c1cancel := q.CallbackWatch(tagFilter("t1"))
	defer c1cancel()
	c2, c2cancel := q.CallbackWatch(tagFilter("t2"))
	defer c2cancel()

	// Publish items on the queue
	q.Publish(testEvent{tags: []string{"t1"}, str: "foo"})
	q.Publish(testEvent{tags: []string{"t2"}, str: "bar"})
	q.Publish(testEvent{tags: []string{"t1", "t2"}, str: "foobar"})
	q.Publish(testEvent{tags: []string{"t3"}, str: "baz"})

	if (<-c1).(testEvent).str != "foo" {
		t.Fatal(`expected "foo" on c1`)
	}

	ev := (<-c1).(testEvent)
	if ev.str != "foobar" {
		t.Fatal(`expected "foobar" on c1`, ev)
	}
	if (<-c2).(testEvent).str != "bar" {
		t.Fatal(`expected "bar" on c2`)
	}
	if (<-c2).(testEvent).str != "foobar" {
		t.Fatal(`expected "foobar" on c2`)
	}

	c1cancel()

	select {
	case _, ok := <-c1:
		if ok {
			t.Fatal("unexpected value on c1")
		}
	default:
		// operation does not proceed after cancel
	}

	q.Publish(testEvent{tags: []string{"t1", "t2"}, str: "foobar"})

	if (<-c2).(testEvent).str != "foobar" {
		t.Fatal(`expected "foobar" on c2`)
	}

	c2cancel()

	select {
	case _, ok := <-c2:
		if ok {
			t.Fatal("unexpected value on c2")
		}
	default:
		// operation does not proceed after cancel
	}
}

func BenchmarkPublish10(b *testing.B) {
	benchmarkWatch(b, 10, 1, false)
}

func BenchmarkPublish100(b *testing.B) {
	benchmarkWatch(b, 100, 1, false)
}

func BenchmarkPublish1000(b *testing.B) {
	benchmarkWatch(b, 1000, 1, false)
}

func BenchmarkPublish10000(b *testing.B) {
	benchmarkWatch(b, 10000, 1, false)
}

func BenchmarkPublish10Listeners4Publishers(b *testing.B) {
	benchmarkWatch(b, 10, 4, false)
}

func BenchmarkPublish100Listeners8Publishers(b *testing.B) {
	benchmarkWatch(b, 100, 8, false)
}

func BenchmarkPublish1000Listeners4Publishers(b *testing.B) {
	benchmarkWatch(b, 1000, 4, false)
}

func BenchmarkPublish1000Listeners64Publishers(b *testing.B) {
	benchmarkWatch(b, 1000, 64, false)
}

func BenchmarkWatch10(b *testing.B) {
	benchmarkWatch(b, 10, 1, true)
}

func BenchmarkWatch100(b *testing.B) {
	benchmarkWatch(b, 100, 1, true)
}

func BenchmarkWatch1000(b *testing.B) {
	benchmarkWatch(b, 1000, 1, true)
}

func BenchmarkWatch10000(b *testing.B) {
	benchmarkWatch(b, 10000, 1, true)
}

func BenchmarkWatch10Listeners4Publishers(b *testing.B) {
	benchmarkWatch(b, 10, 4, true)
}

func BenchmarkWatch100Listeners8Publishers(b *testing.B) {
	benchmarkWatch(b, 100, 8, true)
}

func BenchmarkWatch1000Listeners4Publishers(b *testing.B) {
	benchmarkWatch(b, 1000, 4, true)
}

func BenchmarkWatch1000Listeners64Publishers(b *testing.B) {
	benchmarkWatch(b, 1000, 64, true)
}

func benchmarkWatch(b *testing.B, nlisteners, npublishers int, waitForWatchers bool) {
	q := NewQueue(0)
	var (
		watchersAttached  sync.WaitGroup
		watchersRunning   sync.WaitGroup
		publishersRunning sync.WaitGroup
	)

	for i := 0; i < nlisteners; i++ {
		watchersAttached.Add(1)
		watchersRunning.Add(1)
		go func(n int) {
			w, cancel := q.Watch()
			defer cancel()
			watchersAttached.Done()

			for i := 0; i != n; i++ {
				<-w
			}
			if waitForWatchers {
				watchersRunning.Done()
			}
		}(b.N / npublishers * npublishers)
	}

	// Wait for watchers to be in place before we start publishing events.
	watchersAttached.Wait()

	b.ResetTimer()

	for i := 0; i < npublishers; i++ {
		publishersRunning.Add(1)
		go func(n int) {
			for i := 0; i < n; i++ {
				q.Publish("myevent")
			}
			publishersRunning.Done()
		}(b.N / npublishers)
	}

	publishersRunning.Wait()

	if waitForWatchers {
		watchersRunning.Wait()
	}
}
