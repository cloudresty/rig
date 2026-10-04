package health

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

func call(h http.HandlerFunc) (int, map[string]any, wireBody) {
	rec := httptest.NewRecorder()
	h(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	var raw map[string]any
	var body wireBody
	_ = json.Unmarshal(rec.Body.Bytes(), &raw)
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	return rec.Code, raw, body
}

func fixed(l Level, d string) Check { return func(context.Context) Result { return Result{l, d} } }

// run starts the registry and returns a stop func that cancels and joins.
func run(r *Registry) (stop func()) {
	ctx, cancel := context.WithCancel(context.Background())
	r.Start(ctx)
	return func() { cancel(); r.Wait() }
}

func TestLevelString(t *testing.T) {
	for _, tc := range []struct {
		l    Level
		want string
	}{{OK, "OK"}, {Degraded, "DEGRADED"}, {Failed, "FAIL"}, {Level(9), "Level(9)"}} {
		if got := tc.l.String(); got != tc.want {
			t.Errorf("%d: got %q want %q", tc.l, got, tc.want)
		}
	}
}

func TestIntervalAndFirstRunJitter(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var runs atomic.Int32
		r := New(WithJitter(7))
		r.RegisterReadiness("db", func(context.Context) Result { runs.Add(1); return Result{} }, WithInterval(10*time.Second))
		start := time.Now()
		stop := run(r)
		defer stop()
		first := r.frac("readiness/db", firstRunJitterCap)
		time.Sleep(first - time.Nanosecond)
		synctest.Wait()
		if runs.Load() != 0 {
			t.Fatalf("ran before jittered start (%s): %d", first, runs.Load())
		}
		time.Sleep(time.Nanosecond)
		synctest.Wait()
		if runs.Load() != 1 {
			t.Fatalf("want first run at jitter, got %d", runs.Load())
		}
		time.Sleep(25 * time.Second)
		synctest.Wait()
		if got := runs.Load(); got != 3 {
			t.Fatalf("want 3 runs after 2.5 intervals, got %d (elapsed %s)", got, time.Since(start))
		}
		stop()
	})
}

func TestJitterDiffersPerSeedAndName(t *testing.T) {
	a, b := New(WithJitter(1)), New(WithJitter(2))
	if a.frac("x", time.Hour) == b.frac("x", time.Hour) {
		t.Fatal("different seeds should give different jitter")
	}
	if a.frac("x", time.Hour) == a.frac("y", time.Hour) {
		t.Fatal("different names should give different jitter")
	}
	first := a.frac("x", time.Hour)
	if got := a.frac("x", time.Hour); got != first {
		t.Fatal("jitter must be stable")
	}
}

func TestStaleRule(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var offset atomic.Int64
		clock := func() time.Time { return time.Now().Add(time.Duration(offset.Load())) }
		r := New(WithClock(clock), WithJitter(1))
		r.RegisterReadiness("db", fixed(OK, ""), WithInterval(10*time.Second))
		r.MarkWired()
		stop := run(r)
		defer stop()
		time.Sleep(11 * time.Second)
		synctest.Wait()
		if s := r.Snapshot(Readiness); s.Status != OK {
			t.Fatalf("fresh result should be OK: %+v", s)
		}
		// Time passes without a re-evaluation: exactly at 3x is not stale, past it is.
		stop() // no more evaluations from here on
		st := r.Snapshot(Readiness).Checks["db"]
		age := clock().Sub(st.EvaluatedAt)
		offset.Add(int64(30*time.Second - age)) // age is now exactly 3x: not stale
		if s := r.Snapshot(Readiness); s.Status != OK {
			t.Fatalf("exactly 3x must not be stale: %+v", s)
		}
		offset.Add(int64(time.Second)) // 3x + 1s
		s := r.Snapshot(Readiness)
		if s.Status != Failed || !strings.Contains(s.Checks["db"].Detail, "stale") || s.Code != 503 {
			t.Fatalf("want stale FAIL/503, got %+v", s)
		}
	})
}

func TestStaleAppliesToLivenessAndBypassesGrace(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var offset atomic.Int64
		clock := func() time.Time { return time.Now().Add(time.Duration(offset.Load())) }
		r := New(WithClock(clock), WithJitter(1), WithLivenessHold(0))
		r.RegisterLiveness("w", FromFacts(func() (Level, string) { return OK, "" }), WithInterval(10*time.Second), WithGrace(5))
		r.MarkWired()
		stop := run(r)
		defer stop()
		time.Sleep(11 * time.Second)
		synctest.Wait()
		stop()
		offset.Store(int64(40 * time.Second))
		s := r.Snapshot(Liveness)
		if s.Status != Failed || s.Code != 503 || !strings.Contains(s.Checks["w"].Detail, "stale") {
			t.Fatalf("want liveness stale FAIL, got %+v", s)
		}
	})
}

func TestStaleFromWedgedCheckViaTimeoutThenStale(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		release := make(chan struct{})
		defer close(release)
		r := New(WithJitter(1))
		r.RegisterReadiness("hang", func(context.Context) Result { <-release; return Result{} },
			WithInterval(10*time.Second), WithTimeout(2*time.Second))
		r.MarkWired()
		stop := run(r)
		defer stop()
		time.Sleep(13 * time.Second)
		synctest.Wait()
		if d := r.Snapshot(Readiness).Checks["hang"].Detail; !strings.Contains(d, "timed out") {
			t.Fatalf("want timeout detail, got %q", d)
		}
		time.Sleep(45 * time.Second)
		synctest.Wait()
		if d := r.Snapshot(Readiness).Checks["hang"].Detail; !strings.Contains(d, "stale") {
			t.Fatalf("want stale detail, got %q", d)
		}
		stop()
	})
}

func TestGrace(t *testing.T) {
	for _, tc := range []struct {
		name      string
		grace     int
		wantCodes []int
	}{
		{"default grace 1", 1, []int{503, 503, 503}},
		{"grace 3", 3, []int{200, 200, 503}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				r := New(WithJitter(1))
				r.RegisterReadiness("db", fixed(Failed, "down"), WithInterval(10*time.Second), WithGrace(tc.grace))
				r.MarkWired()
				stop := run(r)
				defer stop()
				time.Sleep(10 * time.Second)
				for i, want := range tc.wantCodes {
					synctest.Wait()
					code, _, body := call(r.ReadyHandler())
					if code != want {
						t.Fatalf("eval %d: code %d want %d (%+v)", i+1, code, want, body)
					}
					if want == 200 && !strings.HasPrefix(body.Checks["db"], "DEGRADED: failing") {
						t.Fatalf("within grace should read as degraded, got %q", body.Checks["db"])
					}
					time.Sleep(10 * time.Second)
				}
				stop()
			})
		})
	}
}

func TestGraceResetsOnRecovery(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var n atomic.Int32
		r := New(WithJitter(1))
		r.RegisterReadiness("db", func(context.Context) Result {
			if c := n.Add(1); c == 3 { // F F OK F F : never 3 consecutive
				return Result{}
			}
			return Result{Failed, "x"}
		}, WithInterval(10*time.Second), WithGrace(3))
		r.MarkWired()
		stop := run(r)
		defer stop()
		for range 5 {
			time.Sleep(10 * time.Second)
			synctest.Wait()
			if code, _, _ := call(r.ReadyHandler()); code != 200 {
				t.Fatalf("run %d: unexpected %d", n.Load(), code)
			}
		}
		stop()
	})
}

func TestLivenessHoldJitter(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := New(WithJitter(42), WithLivenessHold(100*time.Second))
		r.RegisterLiveness("w", FromFacts(func() (Level, string) { return Failed, "wedged" }),
			WithInterval(10*time.Second), WithGrace(1))
		r.MarkWired()
		hold := r.frac("liveness-hold", 100*time.Second)
		if hold <= 0 || hold >= 100*time.Second {
			t.Fatalf("hold %s out of (0,100s)", hold)
		}
		stop := run(r)
		defer stop()
		first := r.frac("liveness/w", firstRunJitterCap) // first evaluation = failure onset (grace 1)
		time.Sleep(first + time.Millisecond)
		synctest.Wait()
		if hold < 2*time.Millisecond {
			t.Fatalf("seed gives a degenerate hold %s; pick another", hold)
		}
		code, _, body := call(r.LiveHandler())
		if code != 200 || !body.Held || body.Status != "FAIL" {
			t.Fatalf("FAIL inside hold must be 200+held, got %d %+v", code, body)
		}
		time.Sleep(hold - 2*time.Millisecond) // 1ms short of the hold
		synctest.Wait()
		if code, _, body = call(r.LiveHandler()); code != 200 || !body.Held {
			t.Fatalf("still inside hold: %d %+v", code, body)
		}
		time.Sleep(2 * time.Millisecond) // 1ms past the hold
		synctest.Wait()
		if code, _, body = call(r.LiveHandler()); code != 503 || body.Held {
			t.Fatalf("after hold want 503, got %d %+v", code, body)
		}
	})
}

func TestLivenessHoldClearsOnRecovery(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var bad atomic.Bool
		bad.Store(true)
		r := New(WithJitter(42), WithLivenessHold(100*time.Second))
		r.RegisterLiveness("w", FromFacts(func() (Level, string) {
			if bad.Load() {
				return Failed, "x"
			}
			return OK, ""
		}), WithInterval(10*time.Second), WithGrace(1))
		r.MarkWired()
		stop := run(r)
		defer stop()
		time.Sleep(11 * time.Second)
		synctest.Wait()
		bad.Store(false)
		time.Sleep(10 * time.Second)
		synctest.Wait()
		if s := r.Snapshot(Liveness); s.Status != OK || s.Held {
			t.Fatalf("recovered: %+v", s)
		}
		if !r.liveFailSince.IsZero() {
			t.Fatal("hold clock should reset on recovery")
		}
		stop()
	})
}

func TestSingleFlightWithCheckIgnoringContext(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		release := make(chan struct{})
		var once sync.Once
		unblock := func() { once.Do(func() { close(release) }) }
		defer unblock()
		var starts, live atomic.Int32
		r := New(WithJitter(1))
		r.RegisterReadiness("hang", func(context.Context) Result { // ignores ctx on purpose
			starts.Add(1)
			live.Add(1)
			defer live.Add(-1)
			<-release
			return Result{}
		}, WithInterval(time.Second), WithTimeout(500*time.Millisecond))
		stop := run(r)
		defer stop()
		time.Sleep(5 * time.Minute)
		synctest.Wait()
		if starts.Load() != 1 || live.Load() != 1 {
			t.Fatalf("want exactly one in-flight run, starts=%d live=%d", starts.Load(), live.Load())
		}
		stop()
		unblock()
		synctest.Wait()
		if live.Load() != 0 {
			t.Fatalf("run should have drained, live=%d", live.Load())
		}
	})
}

func TestCheckHonouringContextTimesOutAndRecovers(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var slow atomic.Bool
		slow.Store(true)
		r := New(WithJitter(1))
		r.RegisterReadiness("db", func(ctx context.Context) Result {
			if slow.Load() {
				<-ctx.Done()
				return Result{Failed, "ctx: " + ctx.Err().Error()}
			}
			return Result{}
		}, WithInterval(10*time.Second), WithTimeout(time.Second))
		r.MarkWired()
		stop := run(r)
		defer stop()
		time.Sleep(11 * time.Second)
		synctest.Wait()
		if code, _, _ := call(r.ReadyHandler()); code != 503 {
			t.Fatalf("want 503 while timing out, got %d", code)
		}
		slow.Store(false)
		time.Sleep(10 * time.Second)
		synctest.Wait()
		if code, _, _ := call(r.ReadyHandler()); code != 200 {
			t.Fatalf("want recovery to 200, got %d", code)
		}
		stop()
	})
}

func TestReadinessPanicIsFailureNotCrash(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := New(WithJitter(1))
		r.RegisterReadiness("boom", func(context.Context) Result { panic("kaboom") }, WithInterval(10*time.Second))
		r.MarkWired()
		stop := run(r)
		defer stop()
		time.Sleep(11 * time.Second)
		synctest.Wait()
		code, _, body := call(r.ReadyHandler())
		if code != 503 || !strings.Contains(body.Checks["boom"], "kaboom") {
			t.Fatalf("got %d %+v", code, body)
		}
		stop()
	})
}

func TestMarkWiredTransitions(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := New(WithJitter(1))
		r.RegisterReadiness("db", fixed(OK, ""), WithInterval(10*time.Second))
		r.RegisterLiveness("w", FromFacts(func() (Level, string) { return OK, "" }), WithInterval(10*time.Second))
		stop := run(r)
		defer stop()
		time.Sleep(11 * time.Second) // everything evaluated, but not wired
		synctest.Wait()

		if code, _, body := call(r.ReadyHandler()); code != 503 || body.Checks["wired"] != "FAIL: starting" {
			t.Fatalf("unwired ready: %d %+v", code, body)
		}
		if code, _, _ := call(r.StartupHandler()); code != 503 {
			t.Fatalf("unwired startup: %d", code)
		}
		if code, _, _ := call(r.LiveHandler()); code != 200 {
			t.Fatalf("unwired live must be OK: %d", code)
		}

		r.MarkWired()
		for name, h := range map[string]http.HandlerFunc{"ready": r.ReadyHandler(), "startup": r.StartupHandler(), "live": r.LiveHandler()} {
			if code, _, body := call(h); code != 200 || body.Status != "OK" {
				t.Fatalf("wired %s: %d %+v", name, code, body)
			}
		}
		stop()
	})
}

func TestStartupWaitsForFirstEvaluationNotHealth(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := New(WithJitter(1))
		r.RegisterReadiness("db", fixed(Failed, "down"), WithInterval(10*time.Second))
		r.MarkWired()
		if code, _, body := call(r.StartupHandler()); code != 503 || body.Checks["db"] != "FAIL: not evaluated" {
			t.Fatalf("before first evaluation: %d %+v", code, body)
		}
		stop := run(r)
		defer stop()
		time.Sleep(11 * time.Second)
		synctest.Wait()
		if code, _, _ := call(r.StartupHandler()); code != 200 {
			t.Fatalf("after evaluation startup must pass even with a failing dependency, got %d", code)
		}
		if code, _, _ := call(r.ReadyHandler()); code != 503 {
			t.Fatalf("readiness must still fail, got %d", code)
		}
		stop()
	})
}

func TestNeverEvaluated(t *testing.T) {
	r := New()
	r.RegisterReadiness("db", fixed(OK, ""))
	r.RegisterLiveness("w", FromFacts(func() (Level, string) { return OK, "" }))
	r.MarkWired() // evaluators deliberately not started
	if s := r.Snapshot(Readiness); s.Status != Failed || s.Checks["db"].Detail != "not evaluated" {
		t.Fatalf("readiness: %+v", s)
	}
	if s := r.Snapshot(Liveness); s.Status != OK || s.Code != 200 {
		t.Fatalf("liveness must be OK before the first evaluation: %+v", s)
	}
}

func TestLivenessOKBeforeFirstEvaluationWithSlowFirstRun(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		release := make(chan struct{})
		defer close(release)
		r := New(WithJitter(1), WithLivenessHold(0))
		r.RegisterLiveness("slow", InProcessCheck{fn: func(context.Context) Result { <-release; return Result{} }},
			WithInterval(time.Minute), WithTimeout(30*time.Second), WithGrace(1))
		r.MarkWired()
		stop := run(r)
		defer stop()
		time.Sleep(time.Second)
		synctest.Wait()
		if code, _, body := call(r.LiveHandler()); code != 200 || body.Checks["slow"] != "OK" {
			t.Fatalf("slow first run must not fail liveness: %d %+v", code, body)
		}
		stop()
	})
}

func TestDegradedIs200(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := New(WithJitter(1))
		r.RegisterReadiness("lag", fixed(Degraded, "behind"), WithInterval(10*time.Second))
		r.RegisterLiveness("w", FromFacts(func() (Level, string) { return Degraded, "slow" }), WithInterval(10*time.Second))
		r.MarkWired()
		stop := run(r)
		defer stop()
		time.Sleep(11 * time.Second)
		synctest.Wait()
		for name, h := range map[string]http.HandlerFunc{"ready": r.ReadyHandler(), "live": r.LiveHandler()} {
			code, _, body := call(h)
			if code != 200 || body.Status != "DEGRADED" {
				t.Fatalf("%s: %d %+v", name, code, body)
			}
		}
		if _, _, body := call(r.ReadyHandler()); body.Checks["lag"] != "DEGRADED: behind" {
			t.Fatalf("got %q", body.Checks["lag"])
		}
		stop()
	})
}

func TestInformationalNeverGates(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := New(WithJitter(1))
		r.RegisterReadiness("db", fixed(OK, ""), WithInterval(10*time.Second))
		r.RegisterReadiness("gemini", fixed(Failed, "upstream down"), WithInterval(10*time.Second), WithKind(Informational))
		r.MarkWired()
		stop := run(r)
		defer stop()
		time.Sleep(11 * time.Second)
		synctest.Wait()
		code, _, body := call(r.ReadyHandler())
		if code != 200 || body.Status != "OK" {
			t.Fatalf("informational FAIL must not gate: %d %+v", code, body)
		}
		if body.Informational["gemini"] != "FAIL: upstream down" || body.Checks["gemini"] != "" {
			t.Fatalf("informational placement: %+v", body)
		}
		stop()
	})
}

func TestInformationalUnevaluatedDoesNotBlockStartup(t *testing.T) {
	r := New()
	r.RegisterReadiness("x", fixed(OK, ""), WithKind(Informational))
	r.MarkWired()
	if s := r.Snapshot(Startup); s.Status != OK {
		t.Fatalf("%+v", s)
	}
}

func TestHandlersNeverRunChecks(t *testing.T) {
	var calls atomic.Int32
	counting := func(context.Context) Result { calls.Add(1); return Result{} }
	r := New()
	r.RegisterReadiness("a", counting)
	r.RegisterLiveness("b", InProcessCheck{fn: func(ctx context.Context) Result { return counting(ctx) }})
	r.MarkWired()
	for range 50 {
		call(r.ReadyHandler())
		call(r.LiveHandler())
		call(r.StartupHandler())
		r.Snapshot(Readiness)
	}
	if calls.Load() != 0 {
		t.Fatalf("probes invoked checks %d times", calls.Load())
	}
}

func TestJSONShapeIsSupersetOfRigHealth(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := New(WithJitter(1))
		r.RegisterReadiness("database", fixed(OK, "ping 3ms"), WithInterval(10*time.Second))
		r.MarkWired()
		stop := run(r)
		defer stop()
		time.Sleep(11 * time.Second)
		synctest.Wait()
		_, raw, _ := call(r.ReadyHandler())
		for _, k := range []string{"status", "checks", "informational", "evaluatedAt"} {
			if _, ok := raw[k]; !ok {
				t.Fatalf("missing key %q in %v", k, raw)
			}
		}
		if raw["status"] != "OK" || raw["checks"].(map[string]any)["database"] != "OK" {
			t.Fatalf("rig-compatible fields changed: %v", raw)
		}
		if raw["details"].(map[string]any)["database"] != "ping 3ms" {
			t.Fatalf("OK detail should surface under details: %v", raw)
		}
		if _, err := time.Parse(time.RFC3339, raw["evaluatedAt"].(string)); err != nil {
			t.Fatal(err)
		}
		stop()
	})
}

func TestRegistrationPanics(t *testing.T) {
	mustPanic := func(name string, f func()) {
		t.Helper()
		defer func() {
			if recover() == nil {
				t.Errorf("%s: want panic", name)
			}
		}()
		f()
	}
	r := New()
	mustPanic("zero InProcessCheck", func() { r.RegisterLiveness("x", InProcessCheck{}) })
	mustPanic("nil readiness", func() { r.RegisterReadiness("x", nil) })
	r.RegisterReadiness("dup", fixed(OK, ""))
	mustPanic("duplicate", func() { r.RegisterReadiness("dup", fixed(OK, "")) })
	mustPanic("nil facts", func() { FromFacts(nil) })
}

func TestRegisterAfterStartIsEvaluated(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := New(WithJitter(1))
		stop := run(r)
		defer stop()
		r.RegisterReadiness("late", fixed(OK, ""), WithInterval(10*time.Second))
		r.MarkWired()
		time.Sleep(11 * time.Second)
		synctest.Wait()
		if s := r.Snapshot(Readiness); s.Status != OK {
			t.Fatalf("%+v", s)
		}
		stop()
	})
}

func TestOutOfRangeLevelIsClamped(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := New(WithJitter(1))
		r.RegisterReadiness("bad", fixed(Level(99), "?"), WithInterval(10*time.Second))
		r.MarkWired()
		stop := run(r)
		defer stop()
		time.Sleep(11 * time.Second)
		synctest.Wait()
		if code, _, _ := call(r.ReadyHandler()); code != 503 {
			t.Fatalf("out-of-range level should clamp to FAIL, got %d", code)
		}
		stop()
	})
}

func TestTimeoutCappedAtInterval(t *testing.T) {
	e := newEntry("x", Readiness, fixed(OK, ""), 1, []CheckOption{WithInterval(time.Second), WithTimeout(time.Minute)})
	if e.timeout != time.Second {
		t.Fatalf("timeout %s", e.timeout)
	}
	e = newEntry("x", Readiness, fixed(OK, ""), 1, []CheckOption{WithInterval(time.Second)})
	if e.timeout != time.Second {
		t.Fatalf("default timeout must also respect interval, got %s", e.timeout)
	}
	e = newEntry("x", Readiness, fixed(OK, ""), 1, nil)
	if e.interval != 10*time.Second || e.timeout != 3*time.Second || e.grace != 1 {
		t.Fatalf("defaults: %+v", e)
	}
}

func TestConcurrentProbesUnderEvaluation(t *testing.T) {
	r := New(WithJitter(1))
	r.RegisterReadiness("a", fixed(OK, ""), WithInterval(time.Millisecond), WithTimeout(time.Millisecond))
	r.RegisterLiveness("b", FromFacts(func() (Level, string) { return Degraded, "x" }), WithInterval(time.Millisecond))
	r.MarkWired()
	stop := run(r)
	defer stop()
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 200 {
				call(r.ReadyHandler())
				call(r.LiveHandler())
			}
		}()
	}
	wg.Wait()
	stop()
}
