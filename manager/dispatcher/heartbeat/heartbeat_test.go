package heartbeat

import (
	"testing"
	"time"
)

func TestHeartbeatBeat(t *testing.T) {
	ch := make(chan struct{})
	hb := New("test", 200*time.Millisecond, func(string) {
		close(ch)
	})
	for i := 0; i < 4; i++ {
		time.Sleep(100 * time.Millisecond)
		hb.Beat()
	}
	hb.Stop("test")
	select {
	case <-ch:
		t.Fatalf("Heartbeat was expired")
	case <-time.After(100 * time.Millisecond):
	}
}

func TestHeartbeatTimeout(t *testing.T) {
	ch := make(chan struct{})
	hb := New("test", 100*time.Millisecond, func(string) {
		close(ch)
	})
	defer hb.Stop("test")
	select {
	case <-ch:
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("timeoutFunc wasn't called in timely fashion")
	}
}

func TestHeartbeatReactivate(t *testing.T) {
	ch := make(chan struct{}, 2)
	hb := New("test", 100*time.Millisecond, func(string) {
		ch <- struct{}{}
	})
	defer hb.Stop("test")
	time.Sleep(200 * time.Millisecond)
	hb.Beat()
	time.Sleep(200 * time.Millisecond)
	for i := 0; i < 2; i++ {
		select {
		case <-ch:
		case <-time.After(500 * time.Millisecond):
			t.Fatalf("timeoutFunc wasn't called in timely fashion")
		}
	}
}

func TestHeartbeatUpdate(t *testing.T) {
	ch := make(chan struct{})
	hb := New("test", 1*time.Second, func(string) {
		close(ch)
	})
	defer hb.Stop("test")
	hb.Update(100 * time.Millisecond)
	hb.Beat()
	time.Sleep(200 * time.Millisecond)
	select {
	case <-ch:
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("timeoutFunc wasn't called in timely fashion")
	}
}
