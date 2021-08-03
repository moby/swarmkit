package gosync

import (
	"sync"
	"time"
)

// TryLocker represents an object that can be locked, unlocked, and implements
// a variant of its lock function that times out if no lock can be obtained in
// a specified duration.
type TryLocker interface {
	sync.Locker

	// TryLock attempts to obtain a lock and times out if no lock
	// can be obtained in the specified duration. A flag is returned
	// indicating whether or not the lock was obtained.
	TryLock(timeout time.Duration) bool
}

// TryMutex is a mutual exclusion lock that implements the TryLocker interface.
// The zero value for a TryMutex is an unlocked mutex.
//
// A TryMutex may be copied after first use.
type TryMutex struct {
	sync.Once
	c chan struct{}
}

func (m *TryMutex) init() {
	m.c = make(chan struct{}, 1)
}

// Lock locks m. If the lock is already in use, the calling goroutine blocks
// until the mutex is available.
func (m *TryMutex) Lock() {
	m.Once.Do(m.init)
	m.c <- struct{}{}
}

// Unlock unlocks m. It is a run-time error if m is not locked on entry to
// Unlock.
//
// A locked TryMutex is not associated with a particular goroutine. It is
// allowed for one goroutine to lock a Mutex and then arrange for another
// goroutine to unlock it.
func (m *TryMutex) Unlock() {
	m.Once.Do(m.init)
	select {
	case <-m.c:
		return
	default:
		panic("gosync: unlock of unlocked mutex")
	}
}

// TryLock attempts to lock m. If no lock can be obtained in the specified
// duration then a false value is returned.
func (m *TryMutex) TryLock(timeout time.Duration) bool {
	m.Once.Do(m.init)

	// If the timeout is zero then do not create a timer.
	if timeout == 0 {
		select {
		case m.c <- struct{}{}:
			return true
		default:
			return false
		}
	}

	// Timeout is greater than zero, so a timer is used.
	timer := time.NewTimer(timeout)
	select {
	case m.c <- struct{}{}:
		timer.Stop()
		return true
	case <-timer.C:
	}
	return false
}
