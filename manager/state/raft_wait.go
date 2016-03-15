package state

import (
	"fmt"
	"sync"
)

type wait struct {
	l sync.Mutex
	m map[uint64]chan interface{}
}

func newWait() *wait {
	return &wait{m: make(map[uint64]chan interface{})}
}

func (w *wait) register(id uint64) <-chan interface{} {
	w.l.Lock()
	defer w.l.Unlock()
	ch := w.m[id]
	if ch == nil {
		ch = make(chan interface{}, 1)
		w.m[id] = ch
	} else {
		panic(fmt.Sprintf("duplicate id %x", id))
	}
	return ch
}

func (w *wait) trigger(id uint64, x interface{}) bool {
	w.l.Lock()
	ch := w.m[id]
	delete(w.m, id)
	w.l.Unlock()
	if ch != nil {
		ch <- x
		close(ch)
		return true
	}
	return false
}

func (w *wait) cancelAll() {
	w.l.Lock()
	defer w.l.Unlock()

	for id, ch := range w.m {
		delete(w.m, id)
		if ch != nil {
			close(ch)
		}
	}
}
