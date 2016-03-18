package watch

import (
	"testing"
	"time"
)

func TestWatch(t *testing.T) {
	// Create a queue
	q := NewQueue(0)

	type testEvent struct {
		tags []string
		str  string
	}

	tagFilter := func(t string) topicFunc {
		return func(event Event) bool {
			testEvent := event.Payload.(testEvent)
			for _, itemTag := range testEvent.tags {
				if t == itemTag {
					return true
				}
			}
			return false
		}
	}

	// Create filtered watchers
	c1 := q.CallbackWatch(tagFilter("t1"))
	c2 := q.CallbackWatch(tagFilter("t2"))

	// Publish items on the queue
	q.Publish(Event{Payload: testEvent{tags: []string{"t1"}, str: "foo"}})
	q.Publish(Event{Payload: testEvent{tags: []string{"t2"}, str: "bar"}})
	q.Publish(Event{Payload: testEvent{tags: []string{"t1", "t2"}, str: "foobar"}})
	q.Publish(Event{Payload: testEvent{tags: []string{"t3"}, str: "baz"}})

	if (<-c1).Payload.(testEvent).str != "foo" {
		t.Fatal(`expected "foo" on c1`)
	}
	if (<-c1).Payload.(testEvent).str != "foobar" {
		t.Fatal(`expected "foobar" on c1`)
	}
	if (<-c2).Payload.(testEvent).str != "bar" {
		t.Fatal(`expected "bar" on c2`)
	}
	if (<-c2).Payload.(testEvent).str != "foobar" {
		t.Fatal(`expected "foobar" on c2`)
	}

	q.StopWatch(c1)

	select {
	case _, ok := <-c1:
		if ok {
			t.Fatal("unexpected value on c1")
		}
	case <-time.After(time.Second):
		t.Fatal("expected c1 to be closed")
	}

	q.Publish(Event{Payload: testEvent{tags: []string{"t1", "t2"}, str: "foobar"}})

	if (<-c2).Payload.(testEvent).str != "foobar" {
		t.Fatal(`expected "foobar" on c2`)
	}

	q.StopWatch(c2)

	select {
	case _, ok := <-c2:
		if ok {
			t.Fatal("unexpected value on c2")
		}
	case <-time.After(time.Second):
		t.Fatal("expected c2 to be closed")
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
			w := q.Watch()
			var i int
			for range q.Watch() {
				i++

				if i >= n {
					break
				}
			}
			q.StopWatch(w)
			if i != n {
				b.Fatalf("expected %v messages, got %v", n, i)
			}
		}(b.N)
	}

	for i := 0; i < b.N; i++ {
		q.Publish(Event{Payload: "myevent"})
	}
}
