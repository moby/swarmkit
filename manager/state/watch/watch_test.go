package watch

import (
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

func BenchmarkWatch10(b *testing.B) {
	benchmarkWatch(b, 10)
}

func BenchmarkWatch100(b *testing.B) {
	benchmarkWatch(b, 100)
}

func BenchmarkWatch1000(b *testing.B) {
	benchmarkWatch(b, 1000)
}

func BenchmarkWatch10000(b *testing.B) {
	benchmarkWatch(b, 10000)
}

func benchmarkWatch(b *testing.B, nlisteners int) {
	q := NewQueue(0)
	for i := 0; i < nlisteners; i++ {
		go func(n int) {
			w, cancel := q.Watch()
			defer cancel()
			var i int

		loop:
			for {
				select {
				case <-w:
					i++

					if i >= n {
						break loop
					}
				}
			}
			if i != n {
				b.Fatalf("expected %v messages, got %v", n, i)
			}
		}(b.N)
	}

	for i := 0; i < b.N; i++ {
		q.Publish("myevent")
	}
}
