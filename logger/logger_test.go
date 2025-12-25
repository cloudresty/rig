package logger

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cloudresty/rig"
	"github.com/cloudresty/rig/requestid"
)

func TestNew_DefaultConfig(t *testing.T) {
	var buf bytes.Buffer

	r := rig.New()
	r.Use(New(Config{
		Output: &buf,
	}))

	r.GET("/test", func(c *rig.Context) error {
		c.Status(http.StatusOK)
		return nil
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	output := buf.String()

	if !strings.Contains(output, "200") {
		t.Error("Expected log to contain status 200")
	}
	if !strings.Contains(output, "GET") {
		t.Error("Expected log to contain method GET")
	}
	if !strings.Contains(output, "/test") {
		t.Error("Expected log to contain path /test")
	}
}

func TestNew_JSONFormat(t *testing.T) {
	var buf bytes.Buffer

	r := rig.New()
	r.Use(New(Config{
		Format: FormatJSON,
		Output: &buf,
	}))

	r.GET("/api/users", func(c *rig.Context) error {
		c.Status(http.StatusOK)
		return nil
	})

	req := httptest.NewRequest(http.MethodGet, "/api/users", nil)
	req.Header.Set("User-Agent", "TestAgent/1.0")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	var entry LogEntry
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("Failed to parse JSON log: %v", err)
	}

	if entry.Status != 200 {
		t.Errorf("Expected status 200, got %d", entry.Status)
	}
	if entry.Method != "GET" {
		t.Errorf("Expected method GET, got %s", entry.Method)
	}
	if entry.Path != "/api/users" {
		t.Errorf("Expected path /api/users, got %s", entry.Path)
	}
	if entry.UserAgent != "TestAgent/1.0" {
		t.Errorf("Expected user agent TestAgent/1.0, got %s", entry.UserAgent)
	}
}

func TestNew_SkipPaths(t *testing.T) {
	var buf bytes.Buffer

	r := rig.New()
	r.Use(New(Config{
		Output:    &buf,
		SkipPaths: []string{"/health", "/ready"},
	}))

	r.GET("/health", func(c *rig.Context) error {
		c.Status(http.StatusOK)
		return nil
	})
	r.GET("/api/users", func(c *rig.Context) error {
		c.Status(http.StatusOK)
		return nil
	})

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if buf.Len() > 0 {
		t.Error("Expected no log for skipped path /health")
	}

	buf.Reset()
	req = httptest.NewRequest(http.MethodGet, "/api/users", nil)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if buf.Len() == 0 {
		t.Error("Expected log for non-skipped path /api/users")
	}
}

func TestNew_ErrorLogging(t *testing.T) {
	var buf bytes.Buffer

	r := rig.New()
	r.Use(New(Config{
		Format: FormatJSON,
		Output: &buf,
	}))

	r.GET("/error", func(c *rig.Context) error {
		return errors.New("something went wrong")
	})

	req := httptest.NewRequest(http.MethodGet, "/error", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	var entry LogEntry
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("Failed to parse JSON log: %v", err)
	}

	if entry.Status != 500 {
		t.Errorf("Expected status 500 for error, got %d", entry.Status)
	}
	if entry.Error != "something went wrong" {
		t.Errorf("Expected error message, got %s", entry.Error)
	}
}


func TestNew_WithRequestID(t *testing.T) {
	var buf bytes.Buffer

	r := rig.New()
	r.Use(requestid.New())
	r.Use(New(Config{
		Format: FormatJSON,
		Output: &buf,
	}))

	r.GET("/test", func(c *rig.Context) error {
		c.Status(http.StatusOK)
		return nil
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	var entry LogEntry
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("Failed to parse JSON log: %v", err)
	}

	if entry.RequestID == "" {
		t.Error("Expected request ID to be present in log")
	}

	if len(entry.RequestID) != 26 {
		t.Errorf("Expected ULID length of 26, got %d", len(entry.RequestID))
	}
}

func TestNew_ClientIP(t *testing.T) {
	var buf bytes.Buffer

	r := rig.New()
	r.Use(New(Config{
		Format: FormatJSON,
		Output: &buf,
	}))

	r.GET("/test", func(c *rig.Context) error {
		c.Status(http.StatusOK)
		return nil
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("X-Forwarded-For", "203.0.113.195, 70.41.3.18")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	var entry LogEntry
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("Failed to parse JSON log: %v", err)
	}

	if entry.ClientIP != "203.0.113.195" {
		t.Errorf("Expected first IP from X-Forwarded-For, got %s", entry.ClientIP)
	}
}

func TestFormatLatency(t *testing.T) {
	tests := []struct {
		name     string
		ns       int64
		contains string
	}{
		{"nanoseconds", 500, "ns"},
		{"microseconds", 1500, "µs"},
		{"milliseconds", 1500000, "ms"},
		{"seconds", 1500000000, "s"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatLatency(time.Duration(tt.ns))
			if !strings.Contains(result, tt.contains) {
				t.Errorf("Expected %s to contain %s", result, tt.contains)
			}
		})
	}
}