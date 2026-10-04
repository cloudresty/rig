package health

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

func TestStaleFloorProtectsShortIntervals(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var offset atomic.Int64
		clock := func() time.Time { return time.Now().Add(time.Duration(offset.Load())) }
		r := New(WithClock(clock), WithJitter(1))
		r.RegisterReadiness("fast", fixed(OK, ""), WithInterval(time.Second))
		r.MarkWired()
		stop := run(r)
		defer stop()
		time.Sleep(3 * time.Second)
		synctest.Wait()
		stop()
		age := clock().Sub(r.Snapshot(Readiness).Checks["fast"].EvaluatedAt)
		offset.Add(int64(29*time.Second - age)) // 29s: past 3x interval, under the 30s floor
		if s := r.Snapshot(Readiness); s.Status != OK {
			t.Fatalf("29s old with 1s interval must not be stale: %+v", s)
		}
		offset.Add(int64(2 * time.Second))
		if s := r.Snapshot(Readiness); s.Status != Failed || !strings.Contains(s.Checks["fast"].Detail, "stale") {
			t.Fatalf("31s old must be stale: %+v", s)
		}
	})
}

func TestLivenessPanicDegradesNeverFails(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := New(WithJitter(1), WithLivenessHold(0))
		r.RegisterLiveness("bug", FromFacts(func() (Level, string) { panic("oops") }), WithInterval(10*time.Second), WithGrace(1))
		r.MarkWired()
		stop := run(r)
		defer stop()
		time.Sleep(15 * time.Second)
		synctest.Wait()
		code, _, body := call(r.LiveHandler())
		if code != 200 || body.Status != "DEGRADED" || body.Checks["bug"] != "DEGRADED: check panicked: oops" {
			t.Fatalf("liveness panic must degrade only: %d %+v", code, body)
		}
	})
}

func TestFirstEvaluationDoesNotWaitAFullInterval(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := New(WithJitter(99))
		r.RegisterReadiness("slowcadence", fixed(OK, ""), WithInterval(time.Minute))
		r.MarkWired()
		stop := run(r)
		defer stop()
		time.Sleep(firstRunJitterCap + time.Millisecond)
		synctest.Wait()
		if code, _, body := call(r.ReadyHandler()); code != 200 {
			t.Fatalf("ready must pass within ~2s of Start: %d %+v", code, body)
		}
		if code, _, _ := call(r.StartupHandler()); code != 200 {
			t.Fatalf("startup must pass within ~2s: %d", code)
		}
	})
}

func TestDeregisterIsPerEntryAndLeakFree(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var a, b atomic.Int32
		r := New(WithJitter(1))
		r.RegisterReadiness("a", func(context.Context) Result { a.Add(1); return Result{} }, WithInterval(10*time.Second))
		r.RegisterReadiness("b", func(context.Context) Result { b.Add(1); return Result{} }, WithInterval(10*time.Second))
		r.MarkWired()
		stop := run(r)
		defer stop()
		time.Sleep(25 * time.Second)
		synctest.Wait()
		if !r.Deregister("a") || r.Deregister("a") {
			t.Fatal("Deregister should report true once, then false")
		}
		aAt := a.Load()
		bAt := b.Load()
		time.Sleep(time.Hour)
		synctest.Wait()
		if a.Load() != aAt {
			t.Fatalf("deregistered check kept running: %d -> %d", aAt, a.Load())
		}
		if b.Load() <= bAt {
			t.Fatal("deregistering a must not stop b")
		}
		if _, ok := r.Snapshot(Readiness).Checks["a"]; ok {
			t.Fatal("a must be gone from the snapshot")
		}
		if r.abnormalExits.Load() != 0 {
			t.Fatal("deregistration is not an abnormal exit")
		}
		// The name can be registered again.
		r.RegisterReadiness("a", fixed(OK, ""), WithInterval(10*time.Second))
		time.Sleep(15 * time.Second)
		synctest.Wait()
		if s := r.Snapshot(Readiness); s.Checks["a"].Evaluated != true {
			t.Fatalf("re-registered check not evaluated: %+v", s)
		}
	})
}

func TestDeregisterRemovesFromEveryScopeAndBeforeStart(t *testing.T) {
	r := New()
	r.RegisterReadiness("x", fixed(OK, ""))
	r.RegisterLiveness("x", FromFacts(func() (Level, string) { return OK, "" }))
	if !r.Deregister("x") {
		t.Fatal("want removal")
	}
	r.MarkWired()
	if len(r.Snapshot(Readiness).Checks) != 0 || len(r.readiness)+len(r.liveness) != 0 {
		t.Fatal("x should be gone from both scopes")
	}
}

func TestReservedNamesPanic(t *testing.T) {
	r := New()
	for name, f := range map[string]func(){
		"wired readiness":    func() { r.RegisterReadiness("wired", fixed(OK, "")) },
		"wired liveness":     func() { r.RegisterLiveness("wired", FromFacts(func() (Level, string) { return OK, "" })) },
		"evaluator liveness": func() { r.RegisterLiveness("evaluator", FromFacts(func() (Level, string) { return OK, "" })) },
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("%s: want panic", name)
				}
			}()
			f()
		}()
	}
}

func TestFreshnessDegradedAndRegistryClock(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var offset atomic.Int64
		clock := func() time.Time { return time.Now().Add(time.Duration(offset.Load())) }
		last := time.Now()
		r := New(WithClock(clock), WithJitter(1))
		r.RegisterReadiness("hard", Freshness("hard", time.Minute, func() time.Time { return last }), WithInterval(10*time.Second))
		r.RegisterReadiness("soft", FreshnessDegraded("soft", time.Minute, func() time.Time { return last }), WithInterval(10*time.Second))
		r.RegisterReadiness("never", FreshnessDegraded("never", time.Minute, func() time.Time { return time.Time{} }), WithInterval(10*time.Second))
		r.MarkWired()
		stop := run(r)
		defer stop()
		time.Sleep(3 * time.Second)
		synctest.Wait()
		if s := r.Snapshot(Readiness); s.Status != Degraded { // "never" is degraded, others fresh
			t.Fatalf("want degraded from never-succeeded soft check: %+v", s)
		}
		offset.Store(int64(time.Hour)) // registry clock says an hour passed
		time.Sleep(10 * time.Second)
		synctest.Wait()
		s := r.Snapshot(Readiness)
		if s.Checks["hard"].Level != Failed || s.Checks["soft"].Level != Degraded {
			t.Fatalf("Freshness must use the registry clock: %+v", s.Checks)
		}
		// Explicit option wins over the registry clock.
		c := Freshness("x", time.Minute, func() time.Time { return last }, WithNow(func() time.Time { return last }))
		if res := c(context.Background()); res.Level != OK {
			t.Fatalf("%+v", res)
		}
	})
}

func TestWatchdogUsesClockOption(t *testing.T) {
	var now atomic.Int64
	clock := func() time.Time { return time.Unix(0, now.Load()) }
	w := NewWatchdog("w", time.Minute, time.Hour, nil, WithNow(clock))
	end := w.Begin("p")
	now.Store(int64(2 * time.Minute))
	if r := w.Readiness()(context.Background()); r.Level != Degraded {
		t.Fatalf("%+v", r)
	}
	end()
}

func TestEvaluatorFailsWhenStartNeverCalled(t *testing.T) {
	var offset atomic.Int64
	clock := func() time.Time { return time.Unix(1000, 0).Add(time.Duration(offset.Load())) }
	r := New(WithClock(clock), WithLivenessHold(0))
	if s := r.Snapshot(Liveness); s.Status != OK || s.Code != 200 {
		t.Fatalf("unwired: %+v", s)
	}
	r.MarkWired()
	offset.Store(int64(29 * time.Second))
	if s := r.Snapshot(Liveness); s.Status != OK || s.Checks["evaluator"].Detail != "waiting for Start" {
		t.Fatalf("within grace: %+v", s)
	}
	offset.Store(int64(31 * time.Second))
	s := r.Snapshot(Liveness)
	if s.Status != Failed || s.Code != 503 || !strings.Contains(s.Checks["evaluator"].Detail, "Start was not called") {
		t.Fatalf("after grace: %+v", s)
	}
	if s := r.Snapshot(Readiness); s.Checks["evaluator"].Detail != "" {
		t.Fatal("evaluator entry belongs to liveness only")
	}
}

func TestEvaluatorShutdownIsDegradedNotFailed(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := New(WithJitter(1), WithLivenessHold(0))
		r.RegisterLiveness("w", FromFacts(func() (Level, string) { return OK, "" }), WithInterval(10*time.Second))
		r.MarkWired()
		ctx, cancel := context.WithCancel(context.Background())
		r.Start(ctx)
		time.Sleep(15 * time.Second)
		synctest.Wait()
		if s := r.Snapshot(Liveness); s.Status != OK {
			t.Fatalf("running: %+v", s)
		}
		cancel()
		r.Wait()
		code, _, body := call(r.LiveHandler())
		if code != 200 || body.Checks["evaluator"] != "DEGRADED: shutting down" {
			t.Fatalf("drain must not fail liveness: %d %+v", code, body)
		}
	})
}

func TestEvaluatorFailsWhenALoopStopsUnexpectedly(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := New(WithJitter(1), WithLivenessHold(0))
		r.RegisterLiveness("w", FromFacts(func() (Level, string) { return OK, "" }), WithInterval(10*time.Second))
		r.MarkWired()
		stop := run(r)
		defer stop()
		time.Sleep(time.Second)
		synctest.Wait()
		r.mu.Lock()
		r.liveness[0].cancel() // a loop ends without Deregister and without shutdown
		r.mu.Unlock()
		synctest.Wait()
		code, _, body := call(r.LiveHandler())
		if code != 503 || !strings.Contains(body.Checks["evaluator"], "stopped unexpectedly") {
			t.Fatalf("got %d %+v", code, body)
		}
	})
}

func TestStartupRequiresLivenessEvaluated(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		release := make(chan struct{})
		defer close(release)
		r := New(WithJitter(1))
		r.RegisterReadiness("db", fixed(OK, ""), WithInterval(10*time.Second))
		r.RegisterLiveness("slow", InProcessCheck{fn: func(context.Context) Result { <-release; return Result{} }},
			WithInterval(time.Minute), WithTimeout(10*time.Second))
		r.MarkWired()
		stop := run(r)
		defer stop()
		time.Sleep(3 * time.Second)
		synctest.Wait()
		code, _, body := call(r.StartupHandler())
		if code != 503 || body.Checks["liveness/slow"] != "FAIL: not evaluated" {
			t.Fatalf("liveness not yet evaluated must hold startup: %d %+v", code, body)
		}
		time.Sleep(15 * time.Second) // the slow check times out: evaluated
		synctest.Wait()
		if code, _, body = call(r.StartupHandler()); code != 200 {
			t.Fatalf("%d %+v", code, body)
		}
	})
}

func TestStartupNoteExplainsOKWithFailingChecks(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := New(WithJitter(1))
		r.RegisterReadiness("db", fixed(Failed, "down"), WithInterval(10*time.Second))
		r.MarkWired()
		stop := run(r)
		defer stop()
		time.Sleep(3 * time.Second)
		synctest.Wait()
		code, raw, _ := call(r.StartupHandler())
		if code != 200 || raw["note"] == nil {
			t.Fatalf("%d %v", code, raw)
		}
	})
}

func TestEvaluatedAtOmittedWhenNothingEvaluated(t *testing.T) {
	r := New()
	r.RegisterReadiness("db", fixed(OK, ""))
	if _, raw, _ := call(r.ReadyHandler()); raw["evaluatedAt"] != nil {
		t.Fatalf("unevaluated: %v", raw)
	}
	if _, raw, _ := call(r.LiveHandler()); raw["evaluatedAt"] != nil {
		t.Fatalf("unwired liveness: %v", raw)
	}
}

func TestLivenessSurvivesLongShutdownDrain(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := New(WithJitter(1), WithLivenessHold(0))
		r.RegisterLiveness("w", FromFacts(func() (Level, string) { return OK, "" }), WithInterval(10*time.Second), WithGrace(1))
		r.RegisterReadiness("db", fixed(OK, ""), WithInterval(10*time.Second))
		r.MarkWired()
		ctx, cancel := context.WithCancel(context.Background())
		r.Start(ctx)
		time.Sleep(15 * time.Second)
		synctest.Wait()
		cancel()
		r.Wait()
		time.Sleep(60 * time.Second) // well past the stale threshold
		code, _, body := call(r.LiveHandler())
		if code != 200 || body.Status != "DEGRADED" || body.Checks["w"] != "DEGRADED: evaluator stopped (shutting down)" {
			t.Fatalf("liveness must stay 200/DEGRADED through the drain: %d %+v", code, body)
		}
		if code, _, body := call(r.ReadyHandler()); code != 503 || !strings.Contains(body.Checks["db"], "stale") {
			t.Fatalf("readiness behaviour during drain is unchanged (stale FAIL): %d %+v", code, body)
		}
	})
}
