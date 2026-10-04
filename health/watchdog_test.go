package health

import (
	"context"
	"strings"
	"testing"
	"testing/synctest"
	"time"
)

func evalLive(w *Watchdog) Result { return w.Liveness().fn(context.Background()) }

func TestWatchdogLivenessLevels(t *testing.T) {
	soft, hard := 10*time.Minute, 30*time.Minute
	for _, tc := range []struct {
		name    string
		elapsed time.Duration
		deps    bool
		want    Level
		contain string
	}{
		{"idle-like start", time.Minute, true, OK, "running 1m0s in phase sync"},
		{"at soft is not past soft", soft, true, OK, ""},
		{"past soft", soft + time.Second, true, Degraded, "past soft limit"},
		{"past soft, deps down", soft + time.Second, false, Degraded, "past soft limit"},
		{"at hard is not past hard", hard, true, Degraded, "past soft limit"},
		{"past hard, deps healthy", hard + time.Second, true, Failed, "past hard limit"},
		{"past hard, deps unhealthy never Failed", hard + time.Hour, false, Degraded, "past hard limit"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				w := NewWatchdog("sync", soft, hard, func() bool { return tc.deps })
				end := w.Begin("sync")
				time.Sleep(tc.elapsed)
				got := evalLive(w)
				if got.Level != tc.want || !strings.Contains(got.Detail, tc.contain) {
					t.Fatalf("got %+v want level %v containing %q", got, tc.want, tc.contain)
				}
				end()
			})
		})
	}
}

func TestWatchdogIdleAndEnd(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		w := NewWatchdog("sync", time.Minute, 2*time.Minute, func() bool { return true })
		if r := evalLive(w); r.Level != OK || r.Detail != "" {
			t.Fatalf("idle: %+v", r)
		}
		end := w.Begin("a")
		time.Sleep(3 * time.Minute)
		if evalLive(w).Level != Failed {
			t.Fatal("want Failed past hard")
		}
		end()
		end() // idempotent
		if evalLive(w).Level != OK {
			t.Fatal("ended unit must clear")
		}
	})
}

func TestWatchdogReadinessNeverFailed(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		w := NewWatchdog("sync", time.Minute, 2*time.Minute, func() bool { return true })
		end := w.Begin("discovery")
		time.Sleep(time.Hour)
		r := w.Readiness()(context.Background())
		if r.Level != Degraded || !strings.Contains(r.Detail, "in phase discovery") {
			t.Fatalf("%+v", r)
		}
		end()
	})
}

func TestWatchdogLongestUnitDecidesAndBeatIsReported(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		w := NewWatchdog("w", time.Minute, time.Hour, nil)
		e1 := w.Begin("old")
		time.Sleep(5 * time.Minute)
		e2 := w.Begin("new")
		w.Beat()
		time.Sleep(time.Second)
		r := evalLive(w)
		if r.Level != Degraded || !strings.Contains(r.Detail, "phase old") || !strings.Contains(r.Detail, "last progress 1s ago") {
			t.Fatalf("%+v", r)
		}
		e1()
		e2()
	})
}

func TestWatchdogWiredThroughRegistryFailsOnlyWithHealthyDeps(t *testing.T) {
	for _, deps := range []bool{true, false} {
		synctest.Test(t, func(t *testing.T) {
			w := NewWatchdog("sync", time.Minute, 2*time.Minute, func() bool { return deps })
			r := New(WithJitter(1), WithLivenessHold(0))
			r.RegisterLiveness("sync", w.Liveness(), WithInterval(10*time.Second))
			r.MarkWired()
			stop := run(r)
			defer stop()
			end := w.Begin("x")
			time.Sleep(10 * time.Minute)
			synctest.Wait()
			code, _, _ := call(r.LiveHandler())
			if deps && code != 503 || !deps && code != 200 {
				t.Fatalf("deps=%v: code %d", deps, code)
			}
			end()
			stop()
		})
	}
}

func TestFreshness(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var last time.Time
		c := Freshness("mongo", time.Minute, func() time.Time { return last })
		if r := c(context.Background()); r.Level != Failed || !strings.Contains(r.Detail, "never") {
			t.Fatalf("%+v", r)
		}
		last = time.Now()
		time.Sleep(30 * time.Second)
		if r := c(context.Background()); r.Level != OK {
			t.Fatalf("%+v", r)
		}
		time.Sleep(31 * time.Second)
		if r := c(context.Background()); r.Level != Failed || !strings.Contains(r.Detail, "mongo") {
			t.Fatalf("%+v", r)
		}
	})
}
