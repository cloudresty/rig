package rig

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestRecover_NoPanic(t *testing.T) {
	r := New()
	r.Use(Recover())

	r.GET("/ok", func(c *Context) error {
		return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/ok", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestRecover_WithPanic(t *testing.T) {
	r := New()
	r.Use(Recover())

	r.GET("/panic", func(_ *Context) error {
		panic("something went wrong")
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/panic", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", w.Code, http.StatusInternalServerError)
	}

	if !strings.Contains(w.Body.String(), "Internal Server Error") {
		t.Errorf("body = %q, should contain error message", w.Body.String())
	}
}

func TestRecover_WithNilPanic(t *testing.T) {
	r := New()
	r.Use(Recover())

	r.GET("/nil-panic", func(_ *Context) error {
		panic("nil panic test")
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/nil-panic", nil)

	// Should not panic - nil panics are valid in Go
	r.ServeHTTP(w, req)
}

func TestCORS_AllowAllOrigins(t *testing.T) {
	r := New()
	r.Use(DefaultCORS())

	r.GET("/api", func(c *Context) error {
		return c.JSON(http.StatusOK, map[string]string{"data": "test"})
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api", nil)
	req.Header.Set("Origin", "https://example.com")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Errorf("Access-Control-Allow-Origin = %q, want %q", got, "*")
	}
}

func TestCORS_SpecificOrigin(t *testing.T) {
	r := New()
	r.Use(CORS(CORSConfig{
		AllowOrigins: []string{"https://allowed.com"},
		AllowMethods: []string{"GET", "POST"},
		AllowHeaders: []string{"Content-Type"},
	}))

	r.GET("/api", func(c *Context) error {
		return c.JSON(http.StatusOK, map[string]string{"data": "test"})
	})

	// Test allowed origin
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api", nil)
	req.Header.Set("Origin", "https://allowed.com")
	r.ServeHTTP(w, req)

	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "https://allowed.com" {
		t.Errorf("Access-Control-Allow-Origin = %q, want %q", got, "https://allowed.com")
	}

	// Test disallowed origin
	w2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodGet, "/api", nil)
	req2.Header.Set("Origin", "https://notallowed.com")
	r.ServeHTTP(w2, req2)

	if got := w2.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("Access-Control-Allow-Origin = %q, want empty for disallowed origin", got)
	}
}

func TestCORS_PreflightRequest(t *testing.T) {
	r := New()
	r.Use(DefaultCORS())

	r.OPTIONS("/api", func(c *Context) error {
		return c.JSON(http.StatusOK, nil)
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodOptions, "/api", nil)
	req.Header.Set("Origin", "https://example.com")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNoContent)
	}

	methods := w.Header().Get("Access-Control-Allow-Methods")
	if !strings.Contains(methods, "GET") || !strings.Contains(methods, "POST") {
		t.Errorf("Access-Control-Allow-Methods = %q, should contain GET and POST", methods)
	}

	headers := w.Header().Get("Access-Control-Allow-Headers")
	if !strings.Contains(headers, "Content-Type") {
		t.Errorf("Access-Control-Allow-Headers = %q, should contain Content-Type", headers)
	}
}

func TestCORS_WildcardSubdomain(t *testing.T) {
	r := New()
	r.Use(CORS(CORSConfig{
		AllowOrigins: []string{"https://*.example.com"},
		AllowMethods: []string{"GET", "POST"},
		AllowHeaders: []string{"Content-Type"},
	}))

	r.GET("/api", func(c *Context) error {
		return c.JSON(http.StatusOK, map[string]string{"data": "test"})
	})

	tests := []struct {
		name       string
		origin     string
		wantAllow  bool
		wantOrigin string
	}{
		{
			name:       "single subdomain matches",
			origin:     "https://app.example.com",
			wantAllow:  true,
			wantOrigin: "https://app.example.com",
		},
		{
			name:       "nested subdomain matches",
			origin:     "https://dev.app.example.com",
			wantAllow:  true,
			wantOrigin: "https://dev.app.example.com",
		},
		{
			name:       "deeply nested subdomain matches",
			origin:     "https://a.b.c.example.com",
			wantAllow:  true,
			wantOrigin: "https://a.b.c.example.com",
		},
		{
			name:      "root domain does not match",
			origin:    "https://example.com",
			wantAllow: false,
		},
		{
			name:      "different domain does not match",
			origin:    "https://app.other.com",
			wantAllow: false,
		},
		{
			name:      "wrong scheme does not match",
			origin:    "http://app.example.com",
			wantAllow: false,
		},
		{
			name:      "suffix attack does not match",
			origin:    "https://evilexample.com",
			wantAllow: false,
		},
		{
			name:      "prefix attack does not match",
			origin:    "https://example.com.evil.com",
			wantAllow: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/api", nil)
			req.Header.Set("Origin", tt.origin)
			r.ServeHTTP(w, req)

			got := w.Header().Get("Access-Control-Allow-Origin")
			if tt.wantAllow {
				if got != tt.wantOrigin {
					t.Errorf("Access-Control-Allow-Origin = %q, want %q", got, tt.wantOrigin)
				}
			} else {
				if got != "" {
					t.Errorf("Access-Control-Allow-Origin = %q, want empty", got)
				}
			}
		})
	}
}

func TestCORS_WildcardWithExactMatch(t *testing.T) {
	r := New()
	r.Use(CORS(CORSConfig{
		AllowOrigins: []string{
			"https://*.development.example.com",
			"https://api.production.example.com", // Exact match
		},
		AllowMethods: []string{"GET"},
		AllowHeaders: []string{"Content-Type"},
	}))

	r.GET("/api", func(c *Context) error {
		return c.JSON(http.StatusOK, map[string]string{"data": "test"})
	})

	tests := []struct {
		name      string
		origin    string
		wantAllow bool
	}{
		{
			name:      "wildcard subdomain matches",
			origin:    "https://app.development.example.com",
			wantAllow: true,
		},
		{
			name:      "exact match works",
			origin:    "https://api.production.example.com",
			wantAllow: true,
		},
		{
			name:      "non-matching origin rejected",
			origin:    "https://app.staging.example.com",
			wantAllow: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/api", nil)
			req.Header.Set("Origin", tt.origin)
			r.ServeHTTP(w, req)

			got := w.Header().Get("Access-Control-Allow-Origin")
			if tt.wantAllow && got == "" {
				t.Errorf("Expected origin to be allowed, got empty header")
			}
			if !tt.wantAllow && got != "" {
				t.Errorf("Expected origin to be rejected, got %q", got)
			}
		})
	}
}

func TestCORS_MultipleWildcardPatterns(t *testing.T) {
	r := New()
	r.Use(CORS(CORSConfig{
		AllowOrigins: []string{
			"https://*.example.com",
			"https://*.example.org",
		},
		AllowMethods: []string{"GET"},
		AllowHeaders: []string{"Content-Type"},
	}))

	r.GET("/api", func(c *Context) error {
		return c.JSON(http.StatusOK, map[string]string{"data": "test"})
	})

	tests := []struct {
		origin    string
		wantAllow bool
	}{
		{"https://app.example.com", true},
		{"https://app.example.org", true},
		{"https://app.example.net", false},
	}

	for _, tt := range tests {
		t.Run(tt.origin, func(t *testing.T) {
			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/api", nil)
			req.Header.Set("Origin", tt.origin)
			r.ServeHTTP(w, req)

			got := w.Header().Get("Access-Control-Allow-Origin")
			if tt.wantAllow && got == "" {
				t.Errorf("Expected origin %s to be allowed", tt.origin)
			}
			if !tt.wantAllow && got != "" {
				t.Errorf("Expected origin %s to be rejected", tt.origin)
			}
		})
	}
}

func TestCORS_InvalidWildcardPatterns(t *testing.T) {
	// Invalid patterns should be silently ignored
	r := New()
	r.Use(CORS(CORSConfig{
		AllowOrigins: []string{
			"*example.com",          // Missing scheme
			"https://example.*.com", // Wildcard not at subdomain position
			"https://*example.com",  // Wildcard not followed by dot
			"https://example.com",   // Valid exact match
		},
		AllowMethods: []string{"GET"},
		AllowHeaders: []string{"Content-Type"},
	}))

	r.GET("/api", func(c *Context) error {
		return c.JSON(http.StatusOK, map[string]string{"data": "test"})
	})

	// Only the exact match should work
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api", nil)
	req.Header.Set("Origin", "https://example.com")
	r.ServeHTTP(w, req)

	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "https://example.com" {
		t.Errorf("Access-Control-Allow-Origin = %q, want %q", got, "https://example.com")
	}

	// Invalid wildcard patterns should not match
	w2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodGet, "/api", nil)
	req2.Header.Set("Origin", "https://sub.example.com")
	r.ServeHTTP(w2, req2)

	if got := w2.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("Invalid wildcard should not match, got %q", got)
	}
}

func TestCORS_WildcardPreflight(t *testing.T) {
	r := New()
	r.Use(CORS(CORSConfig{
		AllowOrigins: []string{"https://*.example.com"},
		AllowMethods: []string{"GET", "POST", "PUT"},
		AllowHeaders: []string{"Content-Type", "Authorization"},
	}))

	r.OPTIONS("/api", func(c *Context) error {
		return nil
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodOptions, "/api", nil)
	req.Header.Set("Origin", "https://app.example.com")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNoContent)
	}

	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "https://app.example.com" {
		t.Errorf("Access-Control-Allow-Origin = %q, want %q", got, "https://app.example.com")
	}

	methods := w.Header().Get("Access-Control-Allow-Methods")
	if !strings.Contains(methods, "PUT") {
		t.Errorf("Access-Control-Allow-Methods = %q, should contain PUT", methods)
	}
}

// --- Timeout Middleware Tests ---

func TestTimeout_HandlerCompletesBeforeTimeout(t *testing.T) {
	r := New()
	r.Use(Timeout(1 * time.Second))

	r.GET("/fast", func(c *Context) error {
		return c.JSON(http.StatusOK, map[string]string{"status": "fast"})
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/fast", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	if resp["status"] != "fast" {
		t.Errorf("status = %q, want %q", resp["status"], "fast")
	}
}

func TestTimeout_HandlerExceedsTimeout(t *testing.T) {
	r := New()
	r.Use(Timeout(50 * time.Millisecond))

	r.GET("/slow", func(c *Context) error {
		// Simulate slow work
		select {
		case <-time.After(200 * time.Millisecond):
			return c.JSON(http.StatusOK, map[string]string{"status": "completed"})
		case <-c.Context().Done():
			return c.Context().Err()
		}
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/slow", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusGatewayTimeout {
		t.Errorf("status = %d, want %d", w.Code, http.StatusGatewayTimeout)
	}

	var resp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	if resp["error"] != "request timed out" {
		t.Errorf("error = %q, want %q", resp["error"], "request timed out")
	}
}

func TestTimeout_ContextCancelledInHandler(t *testing.T) {
	r := New()
	r.Use(Timeout(100 * time.Millisecond))

	contextCancelled := make(chan bool, 1)

	r.GET("/check-context", func(c *Context) error {
		// Wait for context to be cancelled
		<-c.Context().Done()
		contextCancelled <- true
		return c.Context().Err()
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/check-context", nil)
	r.ServeHTTP(w, req)

	// Wait for the goroutine to signal or timeout
	select {
	case <-contextCancelled:
		// Success - context was cancelled
	case <-time.After(500 * time.Millisecond):
		t.Error("context should have been cancelled")
	}
}

func TestTimeoutWithConfig_CustomOnTimeout(t *testing.T) {
	r := New()
	r.Use(TimeoutWithConfig(TimeoutConfig{
		Timeout: 50 * time.Millisecond,
		OnTimeout: func(c *Context) error {
			return c.JSON(http.StatusRequestTimeout, map[string]string{
				"error":  "custom timeout",
				"detail": "request took too long",
			})
		},
	}))

	r.GET("/slow", func(c *Context) error {
		// Properly written handlers should check context
		select {
		case <-time.After(200 * time.Millisecond):
			return c.JSON(http.StatusOK, map[string]string{"status": "completed"})
		case <-c.Context().Done():
			return c.Context().Err()
		}
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/slow", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusRequestTimeout {
		t.Errorf("status = %d, want %d", w.Code, http.StatusRequestTimeout)
	}

	var resp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	if resp["error"] != "custom timeout" {
		t.Errorf("error = %q, want %q", resp["error"], "custom timeout")
	}
}

func TestTimeout_ContextPassedToExternalCalls(t *testing.T) {
	r := New()
	r.Use(Timeout(100 * time.Millisecond))

	var ctxReceived context.Context

	r.GET("/external", func(c *Context) error {
		ctxReceived = c.Context()
		return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/external", nil)
	r.ServeHTTP(w, req)

	if ctxReceived == nil {
		t.Fatal("context should not be nil")
	}

	// Context should have a deadline
	if _, ok := ctxReceived.Deadline(); !ok {
		t.Error("context should have a deadline set by Timeout middleware")
	}
}
