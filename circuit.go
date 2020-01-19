package easybreaker

import (
	"errors"
	"sync/atomic"
	"time"
)

type ToState func(uint32, uint32) bool

func defaultToOpen(total uint32, failures uint32) bool {
	return total > 0 && float64(failures)/float64(total) >= 0.05
}

func defaultToClosed(total uint32, failures uint32) bool {
	return failures == 0
}

const (
	closed   = int32(0) // the request from the application is allowed to pass
	halfOpen = int32(1) // a limited number of requests are allowed to pass
	open     = int32(2) // the request is failed immediately and ErrBreakerOpen returned

	defaultAtLeastReq = 100
)

var ErrBreakerOpen = errors.New("circuit: breaker open")

type Breaker struct {
	state int32 // current state
	until int64 // until timestamp of the interval (in closed state) or cooldown (in open state) period

	interval    int64 // the cyclic period of the closed state
	cooldown    int64 // the period of the open state
	atLeastReqs uint32

	toOpenState   ToState // called on failure being in the closed state
	toClosedState ToState // called after atLeastReqs being in the half-open state

	total    uint32 // requests in total during the interval
	failures uint32 // requests returned an error during the interval

	now func() time.Time
}

type OptionCall func(*Breaker) error

// AtLeastReqs is the number of requests to consider in the half-open state
// before invoking a given toClosed function for decision making.
func WithLeastReqs(atLeastReqs uint32) OptionCall {
	return func(b *Breaker) error {
		if atLeastReqs == 0 {
			return errors.New("circuit: interval must be set")
		}
		b.atLeastReqs = atLeastReqs
		return nil
	}
}

// ToOpen is called whenever a request fails in the closed state.
// If it returns true, the circuit breaker will be placed into the open state.
//
// ToClosed is called in the half-open state once the number of requests reached atLeastReqs.
// If it returns true, the circuit breaker will be placed into the closed state,
// otherwise into the open state.
func WithStateFunc(toOpen, toClosed ToState) OptionCall {
	return func(b *Breaker) error {
		b.toOpenState = toOpen
		b.toClosedState = toClosed
		return nil
	}
}

// Interval is the cyclic period of the closed state.
//
// Cooldown is the period of the open state,
// after which the state of the circuit breaker becomes the half-open.
func New(interval time.Duration, cooldown time.Duration, fns ...OptionCall) (*Breaker, error) {
	if interval.Nanoseconds() == 0 {
		return nil, errors.New("circuit: interval must be set")
	}
	if cooldown.Nanoseconds() == 0 {
		return nil, errors.New("circuit: cooldown must be set")
	}

	b := &Breaker{
		interval: interval.Nanoseconds(),
		cooldown: cooldown.Nanoseconds(),
		until:    time.Now().UnixNano() + interval.Nanoseconds(),
		state:    closed,
	}

	for _, fn := range fns {
		fn(b)
	}

	if b.atLeastReqs == 0 {
		b.atLeastReqs = defaultAtLeastReq
	}
	if b.toOpenState == nil {
		b.toOpenState = defaultToOpen
	}
	if b.toClosedState == nil {
		b.toClosedState = defaultToClosed
	}

	return b, nil
}

func (b *Breaker) Execute(req func() error) error {
	if !b.ready() {
		return ErrBreakerOpen
	}

	atomic.AddUint32(&b.total, 1)
	err := req()

	if err != nil {
		atomic.AddUint32(&b.failures, 1)
		b.onFailure()
	}

	return err
}

func (b *Breaker) ready() bool {
	until := atomic.LoadInt64(&b.until)
	state := atomic.LoadInt32(&b.state)
	now := b.now().UnixNano()

	if state == closed {
		if now < until {
			return true
		}

		// interval period elapsed
		if atomic.CompareAndSwapInt64(&b.until, until, now+b.interval) {
			atomic.StoreUint32(&b.failures, 0)
			atomic.StoreUint32(&b.total, 0)
		}
		return true
	}

	if state == open {
		if now < until {
			return false
		}

		if atomic.CompareAndSwapInt64(&b.until, until, now+b.interval) {
			atomic.StoreUint32(&b.failures, 0)
			atomic.StoreUint32(&b.total, 0)
			atomic.StoreInt32(&b.state, halfOpen)
			return true
		}
		return false
	}

	// in halfOpen state
	total := atomic.LoadUint32(&b.total)
	failures := atomic.LoadUint32(&b.failures)
	atLeastReqs := atomic.LoadUint32(&b.atLeastReqs)

	if total < atLeastReqs {
		return true
	}

	// try to close circuit breaker
	if b.toClosedState(total, failures) {
		if atomic.CompareAndSwapInt64(&b.until, until, now+b.interval) {
			atomic.StoreUint32(&b.failures, 0)
			atomic.StoreUint32(&b.total, 0)
			atomic.StoreInt32(&b.state, closed)
		}
		return true
	}

	// toCloseState failed and beyond atLeastReq, back to the open state
	if atomic.CompareAndSwapInt64(&b.until, until, now+b.cooldown) {
		atomic.StoreUint32(&b.failures, 0)
		atomic.StoreUint32(&b.total, 0)
		atomic.StoreInt32(&b.state, open)
	}
	return false
}

func (b *Breaker) onFailure() {
	until := atomic.LoadInt64(&b.until)
	if atomic.LoadInt32(&b.state) != closed {
		return
	}

	total := atomic.LoadUint32(&b.total)
	failures := atomic.LoadUint32(&b.failures)

	if b.toOpenState(total, failures) {
		now := b.now().UnixNano()
		if atomic.CompareAndSwapInt64(&b.until, until, now+b.cooldown) {
			atomic.StoreUint32(&b.failures, 0)
			atomic.StoreUint32(&b.total, 0)
			atomic.StoreInt32(&b.state, open)
		}
	}
}
