package rig

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNewHealth(t *testing.T) {
	h := NewHealth()
	if h == nil {
		t.Fatal("NewHealth() returned nil")
	}
	if h.readiness == nil {
		t.Error("readiness map is nil")
	}
	if h.liveness == nil {
		t.Error("liveness map is nil")
	}
}

func TestHealth_AddReadinessCheck(t *testing.T) {
	h := NewHealth()
	h.AddReadinessCheck("db", func() error { return nil })

	if len(h.readiness) != 1 {
		t.Errorf("expected 1 readiness check, got %d", len(h.readiness))
	}
	if h.readiness[0].name != "db" {
		t.Errorf("expected check name 'db', got %s", h.readiness[0].name)
	}
}

func TestHealth_AddLivenessCheck(t *testing.T) {
	h := NewHealth()
	h.AddLivenessCheck("ping", func() error { return nil })

	if len(h.liveness) != 1 {
		t.Errorf("expected 1 liveness check, got %d", len(h.liveness))
	}
	if h.liveness[0].name != "ping" {
		t.Errorf("expected check name 'ping', got %s", h.liveness[0].name)
	}
}

func TestHealth_LiveHandler_AllPass(t *testing.T) {
	h := NewHealth()
	h.AddLivenessCheck("ping", func() error { return nil })

	r := New()
	r.GET("/live", h.LiveHandler())

	req := httptest.NewRequest(http.MethodGet, "/live", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rec.Code)
	}

	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if resp["status"] != "OK" {
		t.Errorf("expected status OK, got %v", resp["status"])
	}

	checks := resp["checks"].(map[string]any)
	if checks["ping"] != "OK" {
		t.Errorf("expected ping OK, got %v", checks["ping"])
	}
}

func TestHealth_LiveHandler_OneFails(t *testing.T) {
	h := NewHealth()
	h.AddLivenessCheck("ping", func() error { return nil })
	h.AddLivenessCheck("deadlock", func() error { return errors.New("goroutine stuck") })

	r := New()
	r.GET("/live", h.LiveHandler())

	req := httptest.NewRequest(http.MethodGet, "/live", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status %d, got %d", http.StatusServiceUnavailable, rec.Code)
	}

	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if resp["status"] != "Service Unavailable" {
		t.Errorf("expected status Service Unavailable, got %v", resp["status"])
	}

	checks := resp["checks"].(map[string]any)
	if checks["ping"] != "OK" {
		t.Errorf("expected ping OK, got %v", checks["ping"])
	}
	if checks["deadlock"] != "FAIL: goroutine stuck" {
		t.Errorf("expected deadlock FAIL, got %v", checks["deadlock"])
	}
}

func TestHealth_ReadyHandler_AllPass(t *testing.T) {
	h := NewHealth()
	h.AddReadinessCheck("db", func() error { return nil })
	h.AddReadinessCheck("redis", func() error { return nil })

	r := New()
	r.GET("/ready", h.ReadyHandler())

	req := httptest.NewRequest(http.MethodGet, "/ready", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rec.Code)
	}

	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	checks := resp["checks"].(map[string]any)
	if checks["db"] != "OK" {
		t.Errorf("expected db OK, got %v", checks["db"])
	}
	if checks["redis"] != "OK" {
		t.Errorf("expected redis OK, got %v", checks["redis"])
	}
}

func TestHealth_ReadyHandler_OneFails(t *testing.T) {
	h := NewHealth()
	h.AddReadinessCheck("db", func() error { return nil })
	h.AddReadinessCheck("redis", func() error { return errors.New("connection refused") })

	r := New()
	r.GET("/ready", h.ReadyHandler())

	req := httptest.NewRequest(http.MethodGet, "/ready", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status %d, got %d", http.StatusServiceUnavailable, rec.Code)
	}

	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	checks := resp["checks"].(map[string]any)
	if checks["db"] != "OK" {
		t.Errorf("expected db OK, got %v", checks["db"])
	}
	if checks["redis"] != "FAIL: connection refused" {
		t.Errorf("expected redis FAIL, got %v", checks["redis"])
	}
}

func TestHealth_NoChecks(t *testing.T) {
	h := NewHealth()

	r := New()
	r.GET("/live", h.LiveHandler())
	r.GET("/ready", h.ReadyHandler())

	// Test liveness with no checks
	req := httptest.NewRequest(http.MethodGet, "/live", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("liveness: expected status %d, got %d", http.StatusOK, rec.Code)
	}

	// Test readiness with no checks
	req = httptest.NewRequest(http.MethodGet, "/ready", nil)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("readiness: expected status %d, got %d", http.StatusOK, rec.Code)
	}
}

func TestHealth_WithRouteGroup(t *testing.T) {
	h := NewHealth()
	h.AddLivenessCheck("ping", func() error { return nil })
	h.AddReadinessCheck("db", func() error { return nil })

	r := New()
	hg := r.Group("/health")
	hg.GET("/live", h.LiveHandler())
	hg.GET("/ready", h.ReadyHandler())

	// Test liveness
	req := httptest.NewRequest(http.MethodGet, "/health/live", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("liveness: expected status %d, got %d", http.StatusOK, rec.Code)
	}

	// Test readiness
	req = httptest.NewRequest(http.MethodGet, "/health/ready", nil)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("readiness: expected status %d, got %d", http.StatusOK, rec.Code)
	}
}

// --- Health Check Timeout Tests ---

func TestNewHealthWithConfig(t *testing.T) {
	config := HealthConfig{
		CheckTimeout: 10 * time.Second,
		Parallel:     true,
	}
	h := NewHealthWithConfig(config)

	if h.config.CheckTimeout != 10*time.Second {
		t.Errorf("CheckTimeout = %v, want %v", h.config.CheckTimeout, 10*time.Second)
	}
	if !h.config.Parallel {
		t.Error("Parallel should be true")
	}
}

func TestDefaultHealthConfig(t *testing.T) {
	config := DefaultHealthConfig()

	if config.CheckTimeout != 5*time.Second {
		t.Errorf("CheckTimeout = %v, want %v", config.CheckTimeout, 5*time.Second)
	}
	if config.Parallel {
		t.Error("Parallel should be false by default")
	}
}

func TestHealth_AddReadinessCheckContext(t *testing.T) {
	h := NewHealth()
	h.AddReadinessCheckContext("db", func(ctx context.Context) error {
		return nil
	})

	if len(h.readiness) != 1 {
		t.Errorf("expected 1 readiness check, got %d", len(h.readiness))
	}
	if h.readiness[0].checkFn == nil {
		t.Error("checkFn should not be nil")
	}
}

func TestHealth_CheckTimeout(t *testing.T) {
	config := HealthConfig{
		CheckTimeout: 50 * time.Millisecond,
		Parallel:     false,
	}
	h := NewHealthWithConfig(config)

	// Add a slow check that exceeds the timeout
	h.AddReadinessCheckContext("slow", func(ctx context.Context) error {
		select {
		case <-time.After(200 * time.Millisecond):
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	})

	r := New()
	r.GET("/ready", h.ReadyHandler())

	req := httptest.NewRequest(http.MethodGet, "/ready", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	// Should fail due to timeout
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status %d, got %d", http.StatusServiceUnavailable, rec.Code)
	}

	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	checks := resp["checks"].(map[string]any)
	if checks["slow"] != "FAIL: check timed out" {
		t.Errorf("expected timeout failure, got %v", checks["slow"])
	}
}

func TestHealth_CheckWithCustomTimeout(t *testing.T) {
	config := HealthConfig{
		CheckTimeout: 50 * time.Millisecond, // Global timeout
	}
	h := NewHealthWithConfig(config)

	// Add a check with a longer custom timeout
	h.AddReadinessCheckWithTimeout("slow-ok", 300*time.Millisecond, func(ctx context.Context) error {
		select {
		case <-time.After(100 * time.Millisecond):
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	})

	r := New()
	r.GET("/ready", h.ReadyHandler())

	req := httptest.NewRequest(http.MethodGet, "/ready", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	// Should pass because custom timeout (300ms) > actual time (100ms)
	if rec.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rec.Code)
	}
}

func TestHealth_ParallelChecks(t *testing.T) {
	config := HealthConfig{
		CheckTimeout: 500 * time.Millisecond,
		Parallel:     true,
	}
	h := NewHealthWithConfig(config)

	// Add two slow checks
	h.AddReadinessCheckContext("check1", func(ctx context.Context) error {
		time.Sleep(100 * time.Millisecond)
		return nil
	})
	h.AddReadinessCheckContext("check2", func(ctx context.Context) error {
		time.Sleep(100 * time.Millisecond)
		return nil
	})

	r := New()
	r.GET("/ready", h.ReadyHandler())

	start := time.Now()
	req := httptest.NewRequest(http.MethodGet, "/ready", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	elapsed := time.Since(start)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rec.Code)
	}

	// With parallel execution, both checks should complete in ~100ms, not ~200ms
	if elapsed > 180*time.Millisecond {
		t.Errorf("parallel checks took too long: %v (expected < 180ms)", elapsed)
	}
}

func TestHealth_SimpleCheckWithTimeout(t *testing.T) {
	config := HealthConfig{
		CheckTimeout: 50 * time.Millisecond,
	}
	h := NewHealthWithConfig(config)

	// Add a simple (non-context) slow check
	h.AddLivenessCheck("slow-simple", func() error {
		time.Sleep(200 * time.Millisecond)
		return nil
	})

	r := New()
	r.GET("/live", h.LiveHandler())

	req := httptest.NewRequest(http.MethodGet, "/live", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	// Should fail due to timeout
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status %d, got %d", http.StatusServiceUnavailable, rec.Code)
	}
}
