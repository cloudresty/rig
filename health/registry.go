package health

import (
	"context"
	"fmt"
	"hash/fnv"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

const (
	defaultLivenessHold = 120 * time.Second
	defaultInterval     = 10 * time.Second
	defaultTimeout      = 3 * time.Second
	defaultLiveGrace    = 3
	defaultReadyGrace   = 1
	// staleFactor is how many intervals old a cached result may be before it
	// is reported as stale, subject to the staleFloor.
	staleFactor = 3
	// staleFloor is the minimum stale threshold, so a very short interval
	// cannot make a healthy evaluator look wedged after a few seconds of
	// scheduler or GC delay. Liveness intervals of 10s or more are expected.
	staleFloor = 30 * time.Second
	// firstRunJitterCap bounds the delay before a check's first evaluation, so
	// startup and readiness never wait a full (possibly minute-long) interval.
	// Steady-state ticks are not jittered: the cadence is fixed-rate.
	firstRunJitterCap = 2 * time.Second
	// evaluatorStartGrace is how long after MarkWired Start may still be
	// pending before the built-in "evaluator" liveness entry fails.
	evaluatorStartGrace = 30 * time.Second
	// Reserved check names (synthetic entries owned by the registry).
	wiredName     = "wired"
	evaluatorName = "evaluator"
)

// Registry holds named checks, evaluates them in the background and serves
// the cached results. It is safe for concurrent use and holds no global state.
type Registry struct {
	now  func() time.Time
	seed int64
	hold time.Duration

	// extraScrub runs after the built-in credential scrubber (see WithScrubber).
	extraScrub []func(string) string

	mu            sync.RWMutex
	readiness     []*entry
	liveness      []*entry
	wired         bool
	started       bool
	ctx           context.Context
	liveFailSince time.Time
	wiredAt       time.Time

	// abnormalExits counts evaluator loops that stopped without a Deregister
	// or a shutdown of the Start context; any non-zero value fails the
	// built-in "evaluator" liveness entry.
	abnormalExits atomic.Int32

	wg sync.WaitGroup
}

// RegistryOption configures New.
type RegistryOption func(*Registry)

// WithClock sets the clock used to stamp and age evaluations. Evaluator
// scheduling always uses real timers.
func WithClock(now func() time.Time) RegistryOption {
	return func(r *Registry) { r.now = now }
}

// WithJitter sets the seed for the per-pod jitter (first-run delay and
// liveness hold). The default is derived from the hostname so replicas
// differ and one pod is stable across restarts of the evaluator.
func WithJitter(seed int64) RegistryOption {
	return func(r *Registry) { r.seed = seed }
}

// WithLivenessHold sets the maximum extra time a liveness FAIL is held before
// the endpoint returns 503. Each pod holds for a stable fraction of this in
// [0, max). Default 120s; 0 disables the hold.
func WithLivenessHold(max time.Duration) RegistryOption {
	return func(r *Registry) { r.hold = max }
}

// New returns an empty Registry. Until MarkWired is called readiness and
// startup FAIL with "starting" and liveness is OK.
func New(opts ...RegistryOption) *Registry {
	r := &Registry{
		now:  time.Now,
		seed: hostSeed(),
		hold: defaultLivenessHold,
	}
	for _, o := range opts {
		o(r)
	}
	return r
}

func hostSeed() int64 {
	h, err := os.Hostname()
	if err != nil {
		return 0
	}
	f := fnv.New64a()
	_, _ = f.Write([]byte(h))
	return int64(f.Sum64())
}

// CheckOption configures a single registered check.
type CheckOption func(*entry)

// WithInterval sets how often the check is evaluated (default 10s).
func WithInterval(d time.Duration) CheckOption { return func(e *entry) { e.interval = d } }

// WithTimeout bounds one evaluation (default 3s). It is capped at the
// interval so the cadence, and therefore the stale rule, holds.
func WithTimeout(d time.Duration) CheckOption { return func(e *entry) { e.timeout = d } }

// WithKind sets Gating (default) or Informational.
func WithKind(k Kind) CheckOption { return func(e *entry) { e.kind = k } }

// WithGrace sets how many consecutive failed evaluations are needed before a
// Failed result is reported as FAIL. Until then it is reported as Degraded
// ("failing n/grace"). Default 1 for readiness, 3 for liveness.
func WithGrace(n int) CheckOption { return func(e *entry) { e.grace = n } }

type entry struct {
	name     string
	scope    Scope
	kind     Kind
	interval time.Duration
	timeout  time.Duration
	grace    int
	run      Check

	running atomic.Bool        // single-flight: a run (possibly ignoring ctx) is live
	gone    atomic.Bool        // set by Deregister before the evaluator is cancelled
	cancel  context.CancelFunc // per-entry evaluator cancellation; set under Registry.mu

	mu        sync.Mutex
	evaluated bool
	res       Result
	at        time.Time
	consec    int
}

func newEntry(name string, scope Scope, run Check, defGrace int, opts []CheckOption) *entry {
	e := &entry{name: name, scope: scope, run: run, interval: defaultInterval, timeout: defaultTimeout, grace: defGrace}
	for _, o := range opts {
		o(e)
	}
	if e.interval <= 0 {
		e.interval = defaultInterval
	}
	switch {
	case e.timeout <= 0:
		e.timeout = min(defaultTimeout, e.interval)
	case e.timeout > e.interval:
		e.timeout = e.interval
	}
	if e.grace < 1 {
		e.grace = 1
	}
	return e
}

// RegisterReadiness adds a readiness check. It panics on a nil check, a
// reserved name ("wired") or a name already registered for readiness, as
// http.ServeMux does: all are wiring bugs. To replace a check, Deregister it
// first.
func (r *Registry) RegisterReadiness(name string, c Check, opts ...CheckOption) {
	if c == nil {
		panic("health: RegisterReadiness " + name + ": nil check")
	}
	if name == wiredName {
		panic("health: " + name + " is a reserved check name")
	}
	r.add(newEntry(name, Readiness, c, defaultReadyGrace, opts))
}

// RegisterLiveness adds a liveness check. Only an InProcessCheck is accepted:
// see InProcessCheck. Default grace is 3 consecutive failures. The names
// "wired" and "evaluator" are reserved; like RegisterReadiness it panics on a
// duplicate name.
func (r *Registry) RegisterLiveness(name string, c InProcessCheck, opts ...CheckOption) {
	if name == wiredName || name == evaluatorName {
		panic("health: " + name + " is a reserved check name")
	}
	if c.fn == nil {
		panic("health: RegisterLiveness " + name + ": zero InProcessCheck (use Watchdog.Liveness or FromFacts)")
	}
	r.add(newEntry(name, Liveness, Check(c.fn), defaultLiveGrace, opts))
}

func (r *Registry) add(e *entry) {
	r.mu.Lock()
	defer r.mu.Unlock()
	list := &r.readiness
	if e.scope == Liveness {
		list = &r.liveness
	}
	for _, x := range *list {
		if x.name == e.name {
			panic(fmt.Sprintf("health: duplicate %s check %q", e.scope, e.name))
		}
	}
	*list = append(*list, e)
	if r.started {
		r.spawnLocked(e)
	}
}

// spawnLocked starts e's evaluator under its own cancellable context. Callers
// hold r.mu and have verified r.started.
func (r *Registry) spawnLocked(e *entry) {
	ectx, cancel := context.WithCancel(r.ctx)
	e.cancel = cancel
	r.wg.Add(1)
	go r.loop(ectx, e)
}

// Deregister removes the named check from BOTH scopes (readiness and
// liveness): a name registered in each is removed from each. It stops its
// evaluator (only that one: the cancellation is per entry). It reports
// whether anything was removed. A run already in flight may still finish in
// the background; its result is discarded. If the name is registered again, a
// still-running call of the old entry (a check that ignores its context) can
// overlap the new entry's first run, so check functions must tolerate
// concurrent invocation when they are replaced.
func (r *Registry) Deregister(name string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	removed := false
	drop := func(list *[]*entry) {
		kept := (*list)[:0:0]
		for _, e := range *list {
			if e.name != name {
				kept = append(kept, e)
				continue
			}
			removed = true
			e.gone.Store(true)
			if e.cancel != nil {
				e.cancel()
			}
		}
		*list = kept
	}
	drop(&r.readiness)
	drop(&r.liveness)
	return removed
}

// MarkWired declares that service wiring is complete. Until it is called,
// readiness and startup FAIL with "starting" and liveness is OK.
func (r *Registry) MarkWired() {
	r.mu.Lock()
	if !r.wired {
		r.wired = true
		r.wiredAt = r.now()
	}
	r.mu.Unlock()
}

// Start launches one evaluator goroutine per registered check (and per check
// registered later). Each first run is delayed by a stable per-pod jitter in
// [0, interval). Evaluators exit when ctx is done; Wait joins them. Calling
// Start again is a no-op.
func (r *Registry) Start(ctx context.Context) {
	r.mu.Lock()
	if r.started {
		r.mu.Unlock()
		return
	}
	r.started = true
	r.ctx = ctx
	for _, e := range r.readiness {
		r.spawnLocked(e)
	}
	for _, e := range r.liveness {
		r.spawnLocked(e)
	}
	r.mu.Unlock()
}

// Wait blocks until every evaluator goroutine has exited (after the Start
// context is cancelled). A check function that ignores its context may still
// be running; Wait does not wait for it.
func (r *Registry) Wait() { r.wg.Wait() }

func (r *Registry) startCtxErr() error {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.ctx == nil {
		return nil
	}
	return r.ctx.Err()
}

func (r *Registry) frac(key string, mod time.Duration) time.Duration {
	if mod <= 0 {
		return 0
	}
	f := fnv.New64a()
	_, _ = fmt.Fprintf(f, "%d/%s", r.seed, key)
	return time.Duration(f.Sum64() % uint64(mod))
}

func (r *Registry) loop(ctx context.Context, e *entry) {
	defer r.wg.Done()
	defer func() {
		// A loop may only end because Deregister or the Start context asked
		// it to. Anything else leaves a check silently unevaluated.
		if !e.gone.Load() && r.startCtxErr() == nil {
			r.abnormalExits.Add(1)
		}
	}()
	t := time.NewTimer(r.frac(e.scope.String()+"/"+e.name, min(e.interval, firstRunJitterCap)))
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		began := time.Now()
		r.runOnce(ctx, e)
		t.Reset(max(0, e.interval-time.Since(began)))
	}
}

// runOnce performs one evaluation unless one is already in flight.
func (r *Registry) runOnce(ctx context.Context, e *entry) {
	if !e.running.CompareAndSwap(false, true) {
		return // single-flight: never stack goroutines behind a wedged check
	}
	cctx, cancel := context.WithTimeout(context.WithValue(ctx, clockKey{}, r.now), e.timeout)
	defer cancel()
	panicLevel := Failed
	if e.scope == Liveness {
		// A bug in a health check is not a wedge: failing liveness here would
		// restart every replica at once.
		panicLevel = Degraded
	}
	done := make(chan Result, 1)
	go func() {
		defer e.running.Store(false)
		done <- safeRun(cctx, e.run, panicLevel)
	}()
	timer := time.NewTimer(e.timeout)
	defer timer.Stop()
	select {
	case res := <-done:
		r.record(e, res)
	case <-timer.C:
		select { // a check that honoured the deadline may have just returned
		case res := <-done:
			r.record(e, res)
		default:
			r.record(e, Result{Failed, fmt.Sprintf("check timed out after %s", e.timeout)})
		}
	case <-ctx.Done():
	}
}

// clockKey carries the registry clock to checks (see Freshness).
type clockKey struct{}

func clockFrom(ctx context.Context) func() time.Time {
	if f, ok := ctx.Value(clockKey{}).(func() time.Time); ok {
		return f
	}
	return time.Now
}

func safeRun(ctx context.Context, c Check, panicLevel Level) (res Result) {
	defer func() {
		if p := recover(); p != nil {
			res = Result{panicLevel, fmt.Sprintf("check panicked: %v", p)}
		}
	}()
	res = c(ctx)
	res.Level = res.Level.normalise()
	return res
}

func (r *Registry) record(e *entry, res Result) {
	now := r.now()
	e.mu.Lock()
	e.evaluated, e.res, e.at = true, res, now
	if res.Level == Failed {
		e.consec++
	} else {
		e.consec = 0
	}
	e.mu.Unlock()
	if e.scope == Liveness {
		r.Snapshot(Liveness) // advances the liveness-hold clock at the moment of failure
	}
}

// CheckState is the cached, rule-adjusted state of one check.
type CheckState struct {
	Level       Level
	Detail      string
	Kind        Kind
	Evaluated   bool
	EvaluatedAt time.Time
}

// staleAfter is the age beyond which a cached result is stale.
func staleAfter(interval time.Duration) time.Duration {
	return max(staleFactor*interval, staleFloor)
}

// state applies the grace and stale rules to the cached result.
func (e *entry) state(now time.Time, draining bool) CheckState {
	e.mu.Lock()
	defer e.mu.Unlock()
	st := CheckState{Kind: e.kind}
	if !e.evaluated {
		if e.scope == Liveness {
			// A slow first run must not be able to kill the pod.
			st.Level, st.Detail = OK, "not evaluated yet"
		} else {
			st.Level, st.Detail = Failed, "not evaluated"
		}
		return st
	}
	st.Level, st.Detail, st.Evaluated, st.EvaluatedAt = e.res.Level, e.res.Detail, true, e.at
	if draining && e.scope == Liveness {
		// The evaluators stopped because the service is shutting down, so
		// the cached result will only age. Neither stale nor the last
		// verdict may fail liveness during a long SIGTERM drain.
		st.Level, st.Detail = Degraded, "evaluator stopped (shutting down)"
		return st
	}
	if age := now.Sub(e.at); age > staleAfter(e.interval) {
		st.Level = Failed
		st.Detail = fmt.Sprintf("stale: last evaluated %s ago (interval %s)", age.Round(time.Second), e.interval)
		return st // stale is already debounced by its own 3x window: no grace
	}
	if st.Level == Failed && e.consec < e.grace {
		st.Level = Degraded
		st.Detail = fmt.Sprintf("failing %d/%d: %s", e.consec, e.grace, e.res.Detail)
	}
	return st
}

// Snapshot is a point-in-time view of one probe, built from cached results.
type Snapshot struct {
	Scope Scope
	// Status is the aggregate of the Gating checks.
	Status Level
	// Code is the HTTP status the handler answers with.
	Code int
	// Held reports a liveness FAIL still inside its per-pod jitter hold; Code
	// is 200 while it is true.
	Held          bool
	Checks        map[string]CheckState
	Informational map[string]CheckState
	// EvaluatedAt is the oldest evaluation among the gating checks; zero when
	// nothing has been evaluated (or liveness is not yet enforced).
	EvaluatedAt time.Time
	// Note explains a status that could otherwise be misread (for example a
	// startup probe that is OK while a listed check is FAIL).
	Note string
}

// Snapshot returns the current state of one probe. It reads cached results
// only; it never runs a check.
func (r *Registry) Snapshot(scope Scope) Snapshot {
	now := r.now()
	r.mu.RLock()
	wired, started, wiredAt := r.wired, r.started, r.wiredAt
	shuttingDown := r.ctx != nil && r.ctx.Err() != nil
	ents := r.readiness
	if scope == Liveness {
		ents = r.liveness
	}
	ents = append([]*entry(nil), ents...)
	live := append([]*entry(nil), r.liveness...)
	r.mu.RUnlock()

	s := Snapshot{Scope: scope, Checks: map[string]CheckState{}, Informational: map[string]CheckState{}, Code: 200}
	oldest := time.Time{}
	allEvaluated := true
	for _, e := range ents {
		st := e.state(now, shuttingDown)
		if e.kind == Informational {
			s.Informational[e.name] = st
			continue
		}
		s.Checks[e.name] = st
		s.Status = max(s.Status, st.Level)
		if !st.Evaluated {
			allEvaluated = false
		} else if oldest.IsZero() || st.EvaluatedAt.Before(oldest) {
			oldest = st.EvaluatedAt
		}
	}
	s.EvaluatedAt = oldest

	switch scope {
	case Readiness:
		if !wired {
			s.Checks[wiredName] = CheckState{Level: Failed, Detail: "starting", Kind: Gating}
			s.Status = Failed
		}
	case Startup:
		switch {
		case !wired:
			s.Checks[wiredName] = CheckState{Level: Failed, Detail: "starting", Kind: Gating}
			s.Status = Failed
		default:
			// Every liveness entry must have been evaluated too, so a pod
			// whose liveness evaluators never ran cannot pass startup.
			for _, e := range live {
				if st := e.state(now, false); !st.Evaluated {
					allEvaluated = false
					st.Level, st.Detail = Failed, "not evaluated" // for startup, unlike liveness itself
					s.Checks["liveness/"+e.name] = st
				}
			}
			if !allEvaluated {
				s.Status = Failed
				break
			}
			s.Status = OK // startup is about "finished starting", not dependency health
			for _, st := range s.Checks {
				if st.Level != OK {
					s.Note = "startup is OK once wired and every check has been evaluated once; the check states listed are informative and do not fail startup"
					break
				}
			}
		}
	case Liveness:
		if !wired {
			return Snapshot{Scope: scope, Status: OK, Code: 200,
				Checks:        map[string]CheckState{wiredName: {Level: OK, Detail: "starting; liveness not enforced", Kind: Gating}},
				Informational: map[string]CheckState{}}
		}
		ev := r.evaluatorState(now, started, shuttingDown, wiredAt)
		s.Checks[evaluatorName] = ev
		s.Status = max(s.Status, ev.Level)
		s.Held = r.holdLiveness(s.Status == Failed, now)
		if s.Status == Failed && !s.Held {
			s.Code = 503
		}
		return s
	}
	if s.Status == Failed {
		s.Code = 503
	}
	return s
}

// evaluatorState is the built-in liveness entry the registry owns. It reads
// only in-process counters and runs no user code. It fails when the
// evaluators never started (Start not called within a grace of MarkWired) or
// when a loop ended for a reason other than Deregister or shutdown. Once the
// Start context is cancelled it reports Degraded "shutting down": a pod
// draining on SIGTERM must not be restarted for having stopped its checks.
func (r *Registry) evaluatorState(now time.Time, started, shuttingDown bool, wiredAt time.Time) CheckState {
	st := CheckState{Kind: Gating}
	switch {
	case shuttingDown:
		st.Level, st.Detail = Degraded, "shutting down"
	case !started:
		if age := now.Sub(wiredAt); age > evaluatorStartGrace {
			st.Level = Failed
			st.Detail = fmt.Sprintf("Start was not called within %s of MarkWired (%s ago)", evaluatorStartGrace, age.Round(time.Second))
		} else {
			st.Level, st.Detail = OK, "waiting for Start"
		}
	case r.abnormalExits.Load() > 0:
		st.Level = Failed
		st.Detail = fmt.Sprintf("%d evaluator loop(s) stopped unexpectedly", r.abnormalExits.Load())
	default:
		st.Level = OK
	}
	return st
}

// holdLiveness tracks how long liveness has been failing and reports whether
// the FAIL is still inside this pod's jitter hold.
func (r *Registry) holdLiveness(failed bool, now time.Time) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !failed {
		r.liveFailSince = time.Time{}
		return false
	}
	if r.liveFailSince.IsZero() {
		r.liveFailSince = now
	}
	return now.Sub(r.liveFailSince) < r.frac("liveness-hold", r.hold)
}
