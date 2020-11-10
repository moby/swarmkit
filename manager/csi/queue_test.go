package csi

import (
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

// fakeTimerSource is a fake vqTimerSource that creates fake timers.
type fakeTimerSource struct {
	timers []*fakeTimer
}

// NewTimer creates a new fake timer. Fake timers do not operate off of time,
// but are instead assigned an "order", which is initially equivalent to
// attempt. Calling the advance method will cause new timers to become ready.
func (fs *fakeTimerSource) NewTimer(attempt uint) vqTimer {
	f := &fakeTimer{
		order:   int(attempt),
		attempt: attempt,
		c:       make(chan time.Time, 1),
	}
	if attempt == 0 {
		f.c <- time.Time{}
	}
	fs.timers = append(fs.timers, f)
	return f
}

// advance iterates through all tracked timers, decrementing their order. When
// order reaches 0, the timer has a value written to it, which causes it to
// come available.
func (fs *fakeTimerSource) advance() {
	for _, t := range fs.timers {
		t.order = t.order - 1
		if t.order <= 0 {
			// if no value has yet been written to the timer, then write one
			select {
			case t.c <- time.Time{}:
			default:
			}
		}
	}
}

type fakeTimer struct {
	attempt uint
	order   int
	c       chan time.Time
}

func (f *fakeTimer) Done() <-chan time.Time {
	return f.c
}

func (f *fakeTimer) Stop() bool {
	return false
}

var _ = Describe("volumeQueue", func() {
	var (
		vq         *volumeQueue
		fakeSource *fakeTimerSource
	)

	BeforeEach(func() {
		fakeSource = &fakeTimerSource{
			timers: []*fakeTimer{},
		}

		vq = newVolumeQueue()
		// swap out the timerSource for the fake
		vq.timerSource = fakeSource
	})

	It("should dequeue ready entries", func() {
		vq.enqueue("id1", 0)
		vq.enqueue("id2", 0)
		vq.enqueue("id3", 1)
		vq.enqueue("id4", 2)

		rs1, _ := vq.wait()
		rs2, _ := vq.wait()
		Expect([]string{rs1, rs2}).To(ConsistOf("id1", "id2"))

		// advance the fake source so the next fake timer becomes ready.
		fakeSource.advance()

		rs3, _ := vq.wait()
		Expect(rs3).To(Equal("id3"))

		fakeSource.advance()

		rs4, _ := vq.wait()
		Expect(rs4).To(Equal("id4"))
	})
})
