package requestid

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cloudresty/rig"
)

func TestNew_DefaultConfig(t *testing.T) {
	r := rig.New()
	r.Use(New())

	var capturedID string
	r.GET("/test", func(c *rig.Context) error {
		capturedID = Get(c)
		c.Status(http.StatusOK)
		return nil
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	// Check response header
	headerID := rec.Header().Get(DefaultHeader)
	if headerID == "" {
		t.Error("Expected X-Request-ID header to be set")
	}

	// Check context value
	if capturedID == "" {
		t.Error("Expected request ID to be stored in context")
	}

	// Header and context should match
	if headerID != capturedID {
		t.Errorf("Header ID (%s) and context ID (%s) should match", headerID, capturedID)
	}

	// ULID should be 26 characters
	if len(capturedID) != 26 {
		t.Errorf("Expected ULID length of 26, got %d", len(capturedID))
	}
}

func TestNew_CustomHeader(t *testing.T) {
	r := rig.New()
	r.Use(New(Config{
		Header: "X-Correlation-ID",
	}))

	r.GET("/test", func(c *rig.Context) error {
		c.Status(http.StatusOK)
		return nil
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	// Check custom header
	if rec.Header().Get("X-Correlation-ID") == "" {
		t.Error("Expected X-Correlation-ID header to be set")
	}

	// Default header should not be set
	if rec.Header().Get(DefaultHeader) != "" {
		t.Error("Default header should not be set when custom header is configured")
	}
}

func TestNew_CustomGenerator(t *testing.T) {
	customID := "custom-request-id-12345"

	r := rig.New()
	r.Use(New(Config{
		Generator: func() (string, error) {
			return customID, nil
		},
	}))

	var capturedID string
	r.GET("/test", func(c *rig.Context) error {
		capturedID = Get(c)
		c.Status(http.StatusOK)
		return nil
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if capturedID != customID {
		t.Errorf("Expected custom ID %s, got %s", customID, capturedID)
	}
}

func TestNew_GeneratorError(t *testing.T) {
	r := rig.New()
	r.Use(New(Config{
		Generator: func() (string, error) {
			return "", errors.New("generator failed")
		},
	}))

	var capturedID string
	r.GET("/test", func(c *rig.Context) error {
		capturedID = Get(c)
		c.Status(http.StatusOK)
		return nil
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	// Should have a fallback ID (ULID from fallback)
	if capturedID == "" {
		t.Error("Expected fallback ID when generator fails")
	}
}

func TestNew_TrustProxy_UsesIncomingHeader(t *testing.T) {
	incomingID := "incoming-request-id-from-proxy"

	r := rig.New()
	r.Use(New(Config{
		TrustProxy: true,
	}))

	var capturedID string
	r.GET("/test", func(c *rig.Context) error {
		capturedID = Get(c)
		c.Status(http.StatusOK)
		return nil
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set(DefaultHeader, incomingID)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if capturedID != incomingID {
		t.Errorf("Expected incoming ID %s, got %s", incomingID, capturedID)
	}

	// Response header should also have the incoming ID
	if rec.Header().Get(DefaultHeader) != incomingID {
		t.Errorf("Response header should contain incoming ID")
	}
}

func TestNew_TrustProxy_GeneratesWhenNoHeader(t *testing.T) {
	r := rig.New()
	r.Use(New(Config{
		TrustProxy: true,
	}))

	var capturedID string
	r.GET("/test", func(c *rig.Context) error {
		capturedID = Get(c)
		c.Status(http.StatusOK)
		return nil
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	// No X-Request-ID header set
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	// Should generate a new ID
	if capturedID == "" {
		t.Error("Expected generated ID when no incoming header")
	}

	// Should be a valid ULID (26 chars)
	if len(capturedID) != 26 {
		t.Errorf("Expected ULID length of 26, got %d", len(capturedID))
	}
}

func TestNew_TrustProxyDisabled_IgnoresIncomingHeader(t *testing.T) {
	incomingID := "incoming-request-id-from-proxy"

	r := rig.New()
	r.Use(New(Config{
		TrustProxy: false, // Explicitly disabled
	}))

	var capturedID string
	r.GET("/test", func(c *rig.Context) error {
		capturedID = Get(c)
		c.Status(http.StatusOK)
		return nil
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set(DefaultHeader, incomingID)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	// Should NOT use incoming ID
	if capturedID == incomingID {
		t.Error("Should not use incoming ID when TrustProxy is disabled")
	}

	// Should generate a new ULID
	if len(capturedID) != 26 {
		t.Errorf("Expected ULID length of 26, got %d", len(capturedID))
	}
}

func TestGet_NoRequestID(t *testing.T) {
	r := rig.New()
	// No requestid middleware

	var capturedID string
	r.GET("/test", func(c *rig.Context) error {
		capturedID = Get(c)
		c.Status(http.StatusOK)
		return nil
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	// Should return empty string when no request ID middleware
	if capturedID != "" {
		t.Errorf("Expected empty string when no request ID, got %s", capturedID)
	}
}

func TestNew_UniqueIDs(t *testing.T) {
	r := rig.New()
	r.Use(New())

	ids := make(map[string]bool)
	r.GET("/test", func(c *rig.Context) error {
		id := Get(c)
		if ids[id] {
			t.Errorf("Duplicate ID generated: %s", id)
		}
		ids[id] = true
		c.Status(http.StatusOK)
		return nil
	})

	// Make multiple requests
	for i := 0; i < 100; i++ {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
	}

	if len(ids) != 100 {
		t.Errorf("Expected 100 unique IDs, got %d", len(ids))
	}
}

func TestNew_CustomHeaderWithTrustProxy(t *testing.T) {
	customHeader := "X-Trace-ID"
	incomingID := "trace-12345"

	r := rig.New()
	r.Use(New(Config{
		Header:     customHeader,
		TrustProxy: true,
	}))

	var capturedID string
	r.GET("/test", func(c *rig.Context) error {
		capturedID = Get(c)
		c.Status(http.StatusOK)
		return nil
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set(customHeader, incomingID)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if capturedID != incomingID {
		t.Errorf("Expected incoming ID %s, got %s", incomingID, capturedID)
	}

	if rec.Header().Get(customHeader) != incomingID {
		t.Error("Response should have custom header with incoming ID")
	}
}

