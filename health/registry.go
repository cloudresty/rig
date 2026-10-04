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
	defaultProbeBudget  = 2 * time.Second
	defaultLivenessHold = 120 * time.Second
	defaultInterval     = 10 * time.Second
	defaultTimeout      = 3 * time.Second
	defaultLiveGrace    = 3
	defaultReadyGrace   = 1
	// staleFactor is how many intervals old a cached result may be before it
	// is reported as stale.
	staleFactor = 3
)

// Registry holds named checks, evaluates them in the background and serves
// the cached results. It is safe for concurrent use and holds no global state.
type Registry struct {
	budget time.Duration
	now    func() time.Time
	seed   int64
	hold   time.Duration

	mu            sync.RWMutex
	readiness     []*entry
	liveness      []*entry
	wired         bool
	started       bool
	ctx           context.Context
	liveFailSince time.Time

	wg sync.WaitGroup

	// snap is the snapshot source the handlers use; tests replace it.
	snap func(Scope) Snapshot
}

// RegistryOption configures New.
type RegistryOption func(*Registry)

// WithProbeBudget sets how long a probe handler may take to answer before it
// gives up with a 503 (default 2s, matching the kubelet probe timeout).
func WithProbeBudget(d time.Duration) RegistryOption {
	return func(r *Registry) { r.budget = d }
}

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
		budget: defaultProbeBudget,
		now:    time.Now,
		seed:   hostSeed(),
		hold:   defaultLivenessHold,
	}
	r.snap = r.Snapshot
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

	running atomic.Bool // single-flight: a run (possibly ignoring ctx) is live

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

// RegisterReadiness adds a readiness check. It panics on a nil check or a
// duplicate name, as http.ServeMux does: both are wiring bugs.
func (r *Registry) RegisterReadiness(name string, c Check, opts ...CheckOption) {
	if c == nil {
		panic("health: RegisterReadiness " + name + ": nil check")
	}
	r.add(newEntry(name, Readiness, c, defaultReadyGrace, opts))
}

// RegisterLiveness adds a liveness check. Only an InProcessCheck is accepted:
// see InProcessCheck. Default grace is 3 consecutive failures.
func (r *Registry) RegisterLiveness(name string, c InProcessCheck, opts ...CheckOption) {
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
		r.wg.Add(1)
		go r.loop(r.ctx, e)
	}
}

// MarkWired declares that service wiring is complete. Until it is called,
// readiness and startup FAIL with "starting" and liveness is OK.
func (r *Registry) MarkWired() {
	r.mu.Lock()
	r.wired = true
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
	all := append(append([]*entry(nil), r.readiness...), r.liveness...)
	r.wg.Add(len(all))
	r.mu.Unlock()
	for _, e := range all {
		go r.loop(ctx, e)
	}
}

// Wait blocks until every evaluator goroutine has exited (after the Start
// context is cancelled). A check function that ignores its context may still
// be running; Wait does not wait for it.
func (r *Registry) Wait() { r.wg.Wait() }

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
	t := time.NewTimer(r.frac(e.scope.String()+"/"+e.name, e.interval))
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
	cctx, cancel := context.WithTimeout(ctx, e.timeout)
	defer cancel()
	done := make(chan Result, 1)
	go func() {
		defer e.running.Store(false)
		done <- safeRun(cctx, e.run)
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

func safeRun(ctx context.Context, c Check) (res Result) {
	defer func() {
		if p := recover(); p != nil {
			res = Result{Failed, fmt.Sprintf("check panicked: %v", p)}
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

// state applies the grace and stale rules to the cached result.
func (e *entry) state(now time.Time) CheckState {
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
	if age := now.Sub(e.at); age > staleFactor*e.interval {
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
	// EvaluatedAt is the oldest evaluation among the checks (now if none).
	EvaluatedAt time.Time
}

// Snapshot returns the current state of one probe. It reads cached results
// only; it never runs a check.
func (r *Registry) Snapshot(scope Scope) Snapshot {
	now := r.now()
	r.mu.RLock()
	wired := r.wired
	ents := r.readiness
	if scope == Liveness {
		ents = r.liveness
	}
	ents = append([]*entry(nil), ents...)
	r.mu.RUnlock()

	s := Snapshot{Scope: scope, Checks: map[string]CheckState{}, Informational: map[string]CheckState{}, EvaluatedAt: now, Code: 200}
	oldest := time.Time{}
	allEvaluated := true
	for _, e := range ents {
		st := e.state(now)
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
	if !oldest.IsZero() {
		s.EvaluatedAt = oldest
	}

	switch scope {
	case Readiness:
		if !wired {
			s.Checks["wired"] = CheckState{Level: Failed, Detail: "starting", Kind: Gating}
			s.Status = Failed
		}
	case Startup:
		switch {
		case !wired:
			s.Checks["wired"] = CheckState{Level: Failed, Detail: "starting", Kind: Gating}
			s.Status = Failed
		case !allEvaluated:
			s.Status = Failed
		default:
			s.Status = OK // startup is about "finished starting", not dependency health
		}
	case Liveness:
		if !wired {
			s = Snapshot{Scope: scope, Status: OK, Code: 200, EvaluatedAt: now,
				Checks:        map[string]CheckState{"wired": {Level: OK, Detail: "starting; liveness not enforced", Kind: Gating}},
				Informational: map[string]CheckState{}}
			return s
		}
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
