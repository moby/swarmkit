package heartbeat

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/docker/swarmkit/log"
)

// Heartbeat is simple way to track heartbeats.
type Heartbeat struct {
	timeout int64
	timer   *time.Timer
	ctx     string
}

// New creates new Heartbeat with specified duration. timeoutFunc will be called
// if timeout for heartbeat is expired. Note that in case of timeout you need to
// call Beat() to reactivate Heartbeat.
func New(txid string, timeout time.Duration, timeoutFunc func(string)) *Heartbeat {
	hb := &Heartbeat{
		timeout: int64(timeout),
		timer: time.AfterFunc(timeout, func() {
			log.G(context.Background()).Errorf("firing with transaction %s", txid)
			timeoutFunc(txid)
		}),
		ctx: txid,
	}
	log.G(context.Background()).Infof("timer set with %fs txid:%s", timeout.Seconds(), txid)
	return hb
}

// Beat resets internal timer to zero. It also can be used to reactivate
// Heartbeat after timeout.
func (hb *Heartbeat) Beat() {
	log.G(context.Background()).Errorf("beating with context %s", hb.ctx)
	hb.timer.Reset(time.Duration(atomic.LoadInt64(&hb.timeout)))
}

// Update updates internal timeout to d. It does not do Beat.
func (hb *Heartbeat) Update(d time.Duration) {
	log.G(context.Background()).Errorf("update to %fs with context %s", d.Seconds(), hb.ctx)
	atomic.StoreInt64(&hb.timeout, int64(d))
}

// Stop stops Heartbeat timer.
func (hb *Heartbeat) Stop(method string) {
	log.G(context.Background()).Errorf("%s stopping with context %s", method, hb.ctx)
	hb.timer.Stop()
}
