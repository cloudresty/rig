package rig

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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
		panic(nil)
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

func TestJoinStrings(t *testing.T) {
	tests := []struct {
		name  string
		items []string
		sep   string
		want  string
	}{
		{"empty", []string{}, ", ", ""},
		{"single", []string{"a"}, ", ", "a"},
		{"multiple", []string{"a", "b", "c"}, ", ", "a, b, c"},
		{"different sep", []string{"x", "y"}, "-", "x-y"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := joinStrings(tt.items, tt.sep); got != tt.want {
				t.Errorf("joinStrings() = %q, want %q", got, tt.want)
			}
		})
	}
}
