package rig

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
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
	if _, ok := h.readiness["db"]; !ok {
		t.Error("db check not found")
	}
}

func TestHealth_AddLivenessCheck(t *testing.T) {
	h := NewHealth()
	h.AddLivenessCheck("ping", func() error { return nil })

	if len(h.liveness) != 1 {
		t.Errorf("expected 1 liveness check, got %d", len(h.liveness))
	}
	if _, ok := h.liveness["ping"]; !ok {
		t.Error("ping check not found")
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
