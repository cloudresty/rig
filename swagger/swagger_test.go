package swagger

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cloudresty/rig"
)

const testSpec = `{"openapi":"3.0.0","info":{"title":"Test API","version":"1.0"},"paths":{}}`

func TestNew(t *testing.T) {
	s := New(testSpec)
	if s == nil {
		t.Fatal("New() returned nil")
	}
	if s.specJSON != testSpec {
		t.Error("specJSON not set correctly")
	}
	if s.title != "API Documentation" {
		t.Errorf("expected default title 'API Documentation', got %q", s.title)
	}
	if !s.deepLinking {
		t.Error("expected deepLinking to be true by default")
	}
	if s.docExpansion != "list" {
		t.Errorf("expected docExpansion 'list', got %q", s.docExpansion)
	}
}

func TestSwagger_WithTitle(t *testing.T) {
	s := New(testSpec).WithTitle("My API")
	if s.title != "My API" {
		t.Errorf("expected title 'My API', got %q", s.title)
	}
}

func TestSwagger_WithDeepLinking(t *testing.T) {
	s := New(testSpec).WithDeepLinking(false)
	if s.deepLinking {
		t.Error("expected deepLinking to be false")
	}
}

func TestSwagger_WithDocExpansion(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"list", "list"},
		{"full", "full"},
		{"none", "none"},
		{"invalid", "list"}, // Should keep default
	}

	for _, tt := range tests {
		s := New(testSpec).WithDocExpansion(tt.input)
		if s.docExpansion != tt.want {
			t.Errorf("WithDocExpansion(%q): got %q, want %q", tt.input, s.docExpansion, tt.want)
		}
	}
}

func TestSwagger_Register(t *testing.T) {
	s := New(testSpec)
	r := rig.New()
	s.Register(r, "/docs")

	tests := []struct {
		path       string
		wantStatus int
		wantType   string
	}{
		{"/docs/", http.StatusOK, "text/html"},
		{"/docs/index.html", http.StatusOK, "text/html"},
		{"/docs/doc.json", http.StatusOK, "application/json"},
		{"/docs/swagger-ui.css", http.StatusOK, "text/css"},
		{"/docs/swagger-ui-bundle.js", http.StatusOK, "application/javascript"},
		{"/docs/swagger-ui-standalone-preset.js", http.StatusOK, "application/javascript"},
		{"/docs/favicon-32x32.png", http.StatusOK, "image/png"},
		{"/docs/favicon-16x16.png", http.StatusOK, "image/png"},
		{"/docs", http.StatusMovedPermanently, ""},
	}

	for _, tt := range tests {
		req := httptest.NewRequest(http.MethodGet, tt.path, nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		if rec.Code != tt.wantStatus {
			t.Errorf("%s: got status %d, want %d", tt.path, rec.Code, tt.wantStatus)
		}
		if tt.wantType != "" && !strings.Contains(rec.Header().Get("Content-Type"), tt.wantType) {
			t.Errorf("%s: got content-type %q, want %q", tt.path, rec.Header().Get("Content-Type"), tt.wantType)
		}
	}
}

func TestSwagger_SpecContent(t *testing.T) {
	s := New(testSpec)
	r := rig.New()
	s.Register(r, "/docs")

	req := httptest.NewRequest(http.MethodGet, "/docs/doc.json", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Body.String() != testSpec {
		t.Errorf("spec mismatch: got %s", rec.Body.String())
	}
}

func TestSwagger_IndexContainsConfig(t *testing.T) {
	s := New(testSpec).WithTitle("Test Title").WithDocExpansion("full")
	r := rig.New()
	s.Register(r, "/api-docs")

	req := httptest.NewRequest(http.MethodGet, "/api-docs/", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, "Test Title") {
		t.Error("index.html should contain custom title")
	}
	if !strings.Contains(body, "doc.json") {
		t.Errorf("index.html should contain spec URL, got: %s", body)
	}
	if !strings.Contains(body, `docExpansion: "full"`) {
		t.Error("index.html should contain docExpansion setting")
	}
}

func TestSwagger_RegisterGroup(t *testing.T) {
	s := New(testSpec)
	r := rig.New()
	api := r.Group("/api/v1")
	s.RegisterGroup(api, "/docs")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/docs/", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rec.Code)
	}
	if !strings.Contains(rec.Header().Get("Content-Type"), "text/html") {
		t.Errorf("expected text/html, got %s", rec.Header().Get("Content-Type"))
	}
}

func TestSwagger_NormalizePath(t *testing.T) {
	tests := []struct {
		name       string
		pathPrefix string
		wantPath   string
	}{
		{"empty path defaults to /docs", "", "/docs/"},
		{"path without leading slash", "api-docs", "/api-docs/"},
		{"path with trailing slash", "/docs/", "/docs/"},
		{"normal path", "/swagger", "/swagger/"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := New(testSpec)
			r := rig.New()
			s.Register(r, tt.pathPrefix)

			req := httptest.NewRequest(http.MethodGet, tt.wantPath, nil)
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Errorf("path %q: expected status %d, got %d", tt.wantPath, http.StatusOK, rec.Code)
			}
		})
	}
}

func TestNewFromSwag_Fallback(t *testing.T) {
	// NewFromSwag with non-existent instance should return fallback spec
	s := NewFromSwag("non-existent-instance")
	if s == nil {
		t.Fatal("NewFromSwag returned nil")
	}
	if s.specJSON == "" {
		t.Error("specJSON should not be empty")
	}
	// Should contain fallback spec
	if !strings.Contains(s.specJSON, "openapi") {
		t.Error("fallback spec should contain openapi")
	}
}

func TestSwagger_ChainedBuilders(t *testing.T) {
	s := New(testSpec).
		WithTitle("Chained API").
		WithDeepLinking(false).
		WithDocExpansion("full")

	if s.title != "Chained API" {
		t.Errorf("expected title 'Chained API', got %q", s.title)
	}
	if s.deepLinking {
		t.Error("expected deepLinking to be false")
	}
	if s.docExpansion != "full" {
		t.Errorf("expected docExpansion 'full', got %q", s.docExpansion)
	}
}
