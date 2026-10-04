package health

import (
	"context"
	"fmt"
	"time"
)

// InProcessCheck is a liveness check that reads only in-process state. It is
// deliberately not a bare func type: the only ways to obtain one are
// Watchdog.Liveness and FromFacts, so Registry.RegisterLiveness cannot be
// handed a function that does I/O (a database ping, an HTTP call) without the
// caller explicitly laundering it through FromFacts, which takes no context.
type InProcessCheck struct {
	fn func(ctx context.Context) Result
}

// FromFacts adapts a function to an InProcessCheck. It can wrap any func: the
// type does not inspect what f does. What it prevents is handing
// RegisterLiveness a raw Check (which receives a context and is built for
// I/O) by accident, so crossing into liveness is always an explicit,
// reviewable FromFacts call. f takes no context; it must read only facts the
// process already holds (for example a client library's pure in-memory
// verdict) and must not block on I/O. A panic in f degrades liveness; it
// never fails it.
func FromFacts(f func() (Level, string)) InProcessCheck {
	if f == nil {
		panic("health: FromFacts requires a non-nil function")
	}
	return InProcessCheck{fn: func(context.Context) Result {
		l, d := f()
		return Result{Level: l, Detail: d}
	}}
}

// ClockOption overrides the clock used by Freshness and NewWatchdog. Pass the
// same function given to WithClock so every component ages data against one
// clock.
type ClockOption func(*clockCfg)

type clockCfg struct{ now func() time.Time }

// WithNow sets the clock for Freshness or NewWatchdog. It MUST be the same
// function passed to the registry's WithClock, otherwise ages computed here
// and the registry's stale and grace rules disagree about what time it is.
func WithNow(now func() time.Time) ClockOption { return func(c *clockCfg) { c.now = now } }

// Freshness returns a Check that is OK while lastAt() is no older than
// maxAge, and Failed otherwise (including when lastAt() is the zero time,
// meaning "never succeeded"). It reads a timestamp that a background loop
// keeps current, so the check itself does no I/O: pair it with a goroutine
// that pings the dependency and stores the success time.
//
// The clock is, in order: a WithNow option, the clock of the Registry that is
// evaluating it (WithClock), time.Now. Use FreshnessDegraded where staleness
// should be worrying but not gating; Informational is not a substitute, since
// the informational map does not affect the reported status.
func Freshness(name string, maxAge time.Duration, lastAt func() time.Time, opts ...ClockOption) Check {
	return freshness(name, maxAge, lastAt, Failed, opts)
}

// FreshnessDegraded is Freshness that reports Degraded (HTTP 200, visible in
// the aggregate status) instead of Failed when the timestamp is stale or
// missing.
func FreshnessDegraded(name string, maxAge time.Duration, lastAt func() time.Time, opts ...ClockOption) Check {
	return freshness(name, maxAge, lastAt, Degraded, opts)
}

func freshness(name string, maxAge time.Duration, lastAt func() time.Time, stale Level, opts []ClockOption) Check {
	var cfg clockCfg
	for _, o := range opts {
		o(&cfg)
	}
	return func(ctx context.Context) Result {
		now := cfg.now
		if now == nil {
			now = clockFrom(ctx)
		}
		t := lastAt()
		if t.IsZero() {
			return Result{stale, name + ": never succeeded"}
		}
		age := now().Sub(t)
		if age > maxAge {
			return Result{stale, fmt.Sprintf("%s: last success %s ago (max %s)", name, age.Round(time.Second), maxAge)}
		}
		return Result{OK, fmt.Sprintf("%s: last success %s ago", name, age.Round(time.Second))}
	}
}
