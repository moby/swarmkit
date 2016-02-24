package watch

import (
	"testing"
	"time"
)

func TestWatch(t *testing.T) {
	// Create a queue
	q := NewQueue(0)

	// Create filtered watchers
	c1 := q.FilteredWatch(&Filter{Tags: []string{"t1"}})
	c2 := q.FilteredWatch(&Filter{Tags: []string{"t2"}})

	// Publish items on the queue
	q.Publish(Event{Tags: []string{"t1"}, Payload: "foo"})
	q.Publish(Event{Tags: []string{"t2"}, Payload: "bar"})
	q.Publish(Event{Tags: []string{"t1", "t2"}, Payload: "foobar"})
	q.Publish(Event{Tags: []string{"t3"}, Payload: "baz"})

	if (<-c1).Payload.(string) != "foo" {
		t.Fatal(`expected "foo" on c1`)
	}
	if (<-c1).Payload.(string) != "foobar" {
		t.Fatal(`expected "foobar" on c1`)
	}
	if (<-c2).Payload.(string) != "bar" {
		t.Fatal(`expected "bar" on c2`)
	}
	if (<-c2).Payload.(string) != "foobar" {
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

	q.Publish(Event{Tags: []string{"t1", "t2"}, Payload: "foobar"})

	if (<-c2).Payload.(string) != "foobar" {
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
