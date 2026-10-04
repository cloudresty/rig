package health

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// Watchdog tracks units of work (a sync run, a batch, a loop iteration) and
// reports when one has been running too long. It is the generalisation of a
// per-service "run tracker": a unit past the soft limit is Degraded; a unit
// past the hard limit is a liveness Failed, but only while the pod's
// dependencies are healthy, so an outage never causes restarts.
type Watchdog struct {
	name        string
	soft, hard  time.Duration
	depsHealthy func() bool
	now         func() time.Time

	mu       sync.Mutex
	seq      uint64
	inflight map[uint64]unit
	lastBeat time.Time
}

type unit struct {
	label string
	start time.Time
}

// NewWatchdog returns a Watchdog. soft and hard are the Degraded and Failed
// thresholds for the longest in-flight unit (hard should be the unit's budget
// plus a margin). depsHealthy reports whether the pod's gating dependencies
// are currently healthy; liveness never fails while it returns false. A nil
// depsHealthy means "no gating dependencies" and is treated as always true.
func NewWatchdog(name string, soft, hard time.Duration, depsHealthy func() bool) *Watchdog {
	if depsHealthy == nil {
		depsHealthy = func() bool { return true }
	}
	return &Watchdog{
		name:        name,
		soft:        soft,
		hard:        hard,
		depsHealthy: depsHealthy,
		now:         time.Now,
		inflight:    make(map[uint64]unit),
	}
}

// Begin marks a unit of work as started; label names the phase and appears in
// the detail line. Call the returned end function when the unit finishes
// (defer it). end is idempotent.
func (w *Watchdog) Begin(label string) (end func()) {
	w.mu.Lock()
	w.seq++
	id := w.seq
	w.inflight[id] = unit{label: label, start: w.now()}
	w.mu.Unlock()
	return func() {
		w.mu.Lock()
		delete(w.inflight, id)
		w.mu.Unlock()
	}
}

// Beat records progress. It is informational: it appears in the detail line
// ("last progress 3s ago") but does not extend the soft or hard limits, which
// bound the total running time of a unit.
func (w *Watchdog) Beat() {
	w.mu.Lock()
	w.lastBeat = w.now()
	w.mu.Unlock()
}

// longest returns the longest-running in-flight unit.
func (w *Watchdog) longest() (label string, elapsed time.Duration, since string, ok bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	now := w.now()
	for _, u := range w.inflight {
		if d := now.Sub(u.start); !ok || d > elapsed {
			label, elapsed, ok = u.label, d, true
		}
	}
	if ok && !w.lastBeat.IsZero() {
		since = fmt.Sprintf(", last progress %s ago", now.Sub(w.lastBeat).Round(time.Second))
	}
	return label, elapsed, since, ok
}

func (w *Watchdog) evaluate(depsGate bool) Result {
	label, elapsed, beat, ok := w.longest()
	if !ok {
		return Result{OK, ""}
	}
	run := fmt.Sprintf("%s: running %s in phase %s%s", w.name, elapsed.Round(time.Second), label, beat)
	switch {
	case elapsed > w.hard:
		if depsGate && w.depsHealthy() {
			return Result{Failed, run + fmt.Sprintf(", past hard limit %s with dependencies healthy", w.hard)}
		}
		return Result{Degraded, run + fmt.Sprintf(", past hard limit %s", w.hard)}
	case elapsed > w.soft:
		return Result{Degraded, run + fmt.Sprintf(", past soft limit %s", w.soft)}
	}
	return Result{OK, run}
}

// Liveness returns the liveness check: Failed iff an in-flight unit has run
// longer than the hard limit AND depsHealthy() is true; Degraded past the soft
// limit (or past hard while dependencies are unhealthy).
func (w *Watchdog) Liveness() InProcessCheck {
	return InProcessCheck{fn: func(context.Context) Result { return w.evaluate(true) }}
}

// Readiness returns a readiness check that is Degraded when a unit is
// overrunning and never Failed: a slow run is not a reason to pull a healthy
// worker out of service.
func (w *Watchdog) Readiness() Check {
	return func(context.Context) Result { return w.evaluate(false) }
}
