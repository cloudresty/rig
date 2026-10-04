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

// FromFacts adapts a function over facts already held in memory (for example
// a client library's pure "assess" verdict) to an InProcessCheck. The function
// takes no context on purpose: it must not block on I/O.
func FromFacts(f func() (Level, string)) InProcessCheck {
	if f == nil {
		panic("health: FromFacts requires a non-nil function")
	}
	return InProcessCheck{fn: func(context.Context) Result {
		l, d := f()
		return Result{Level: l, Detail: d}
	}}
}

// Freshness returns a readiness-style Check that is OK while lastAt() is no
// older than maxAge, and Failed otherwise (including when lastAt() is the
// zero time, meaning "never succeeded"). It reads a timestamp that a
// background loop keeps current, so the check itself does no I/O: pair it
// with a goroutine that pings the dependency and stores the success time.
// Register it with WithKind(Informational) where staleness should only be
// reported.
func Freshness(name string, maxAge time.Duration, lastAt func() time.Time) Check {
	return func(context.Context) Result {
		t := lastAt()
		if t.IsZero() {
			return Result{Failed, name + ": never succeeded"}
		}
		age := time.Since(t)
		if age > maxAge {
			return Result{Failed, fmt.Sprintf("%s: last success %s ago (max %s)", name, age.Round(time.Second), maxAge)}
		}
		return Result{OK, fmt.Sprintf("%s: last success %s ago", name, age.Round(time.Second))}
	}
}
