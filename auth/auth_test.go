package auth_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cloudresty/rig"
	"github.com/cloudresty/rig/auth"
)

// Helper to create a test router with a protected endpoint
func setupRouter(middleware rig.MiddlewareFunc) *rig.Router {
	r := rig.New()

	api := r.Group("/api")
	api.Use(middleware)
	api.GET("/protected", func(c *rig.Context) error {
		return c.JSON(http.StatusOK, map[string]any{
			"identity": auth.GetIdentity(c),
			"method":   auth.GetMethod(c),
		})
	})

	return r
}

// --- API Key Tests ---

func TestAPIKey_ValidKey_Header(t *testing.T) {
	r := setupRouter(auth.APIKey(auth.APIKeyConfig{
		Name: "X-API-Key",
		Validator: func(key string) (string, bool) {
			if key == "valid-key" {
				return "test-service", true
			}
			return "", false
		},
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/protected", nil)
	req.Header.Set("X-API-Key", "valid-key")
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}

	var resp map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)

	if resp["identity"] != "test-service" {
		t.Errorf("expected identity 'test-service', got %v", resp["identity"])
	}
	if resp["method"] != "api_key" {
		t.Errorf("expected method 'api_key', got %v", resp["method"])
	}
}

func TestAPIKey_ValidKey_Query(t *testing.T) {
	r := setupRouter(auth.APIKey(auth.APIKeyConfig{
		Source: "query",
		Name:   "api_key",
		Validator: func(key string) (string, bool) {
			if key == "valid-key" {
				return "query-service", true
			}
			return "", false
		},
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/protected?api_key=valid-key", nil)
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}
}

func TestAPIKey_MissingKey(t *testing.T) {
	r := setupRouter(auth.APIKey(auth.APIKeyConfig{
		Validator: func(key string) (string, bool) {
			return "", false
		},
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/protected", nil)
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401, got %d", rec.Code)
	}
}

func TestAPIKey_InvalidKey(t *testing.T) {
	r := setupRouter(auth.APIKey(auth.APIKeyConfig{
		Validator: func(key string) (string, bool) {
			return "", false
		},
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/protected", nil)
	req.Header.Set("X-API-Key", "wrong-key")
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401, got %d", rec.Code)
	}
}

func TestAPIKey_CustomOnError(t *testing.T) {
	r := setupRouter(auth.APIKey(auth.APIKeyConfig{
		Validator: func(key string) (string, bool) { return "", false },
		OnError: func(c *rig.Context) error {
			return c.JSON(http.StatusForbidden, map[string]string{"custom": "error"})
		},
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/protected", nil)
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("expected status 403, got %d", rec.Code)
	}

	var resp map[string]string
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp["custom"] != "error" {
		t.Errorf("expected custom error response")
	}
}

func TestAPIKeySimple(t *testing.T) {
	r := setupRouter(auth.APIKeySimple("key1", "key2", "key3"))

	// Valid key
	req := httptest.NewRequest(http.MethodGet, "/api/protected", nil)
	req.Header.Set("X-API-Key", "key2")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200 for valid key, got %d", rec.Code)
	}

	// Invalid key
	req = httptest.NewRequest(http.MethodGet, "/api/protected", nil)
	req.Header.Set("X-API-Key", "invalid")
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401 for invalid key, got %d", rec.Code)
	}
}

// --- Bearer Token Tests ---

func TestBearer_ValidToken(t *testing.T) {
	r := setupRouter(auth.Bearer(auth.BearerConfig{
		Validator: func(token string) (string, bool) {
			if token == "valid-token" {
				return "user-123", true
			}
			return "", false
		},
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/protected", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}

	var resp map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)

	if resp["identity"] != "user-123" {
		t.Errorf("expected identity 'user-123', got %v", resp["identity"])
	}
	if resp["method"] != "bearer" {
		t.Errorf("expected method 'bearer', got %v", resp["method"])
	}
}

func TestBearer_CaseInsensitive(t *testing.T) {
	r := setupRouter(auth.Bearer(auth.BearerConfig{
		Validator: func(token string) (string, bool) {
			return "user", token == "token"
		},
	}))

	// Test lowercase "bearer"
	req := httptest.NewRequest(http.MethodGet, "/api/protected", nil)
	req.Header.Set("Authorization", "bearer token")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200 for lowercase bearer, got %d", rec.Code)
	}

	// Test mixed case "BEARER"
	req = httptest.NewRequest(http.MethodGet, "/api/protected", nil)
	req.Header.Set("Authorization", "BEARER token")
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200 for uppercase BEARER, got %d", rec.Code)
	}
}

func TestBearer_MissingHeader(t *testing.T) {
	r := setupRouter(auth.Bearer(auth.BearerConfig{
		Validator: func(token string) (string, bool) { return "", false },
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/protected", nil)
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401, got %d", rec.Code)
	}

	// Check WWW-Authenticate header is set
	wwwAuth := rec.Header().Get("WWW-Authenticate")
	if wwwAuth == "" {
		t.Error("expected WWW-Authenticate header to be set")
	}
}

func TestBearer_InvalidToken(t *testing.T) {
	r := setupRouter(auth.Bearer(auth.BearerConfig{
		Validator: func(token string) (string, bool) { return "", false },
		Realm:     "TestRealm",
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/protected", nil)
	req.Header.Set("Authorization", "Bearer invalid-token")
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401, got %d", rec.Code)
	}

	// Check WWW-Authenticate header includes realm and error
	wwwAuth := rec.Header().Get("WWW-Authenticate")
	if wwwAuth == "" {
		t.Error("expected WWW-Authenticate header")
	}
	if !contains(wwwAuth, "TestRealm") {
		t.Errorf("expected WWW-Authenticate to contain realm, got %s", wwwAuth)
	}
}

func TestBearer_EmptyToken(t *testing.T) {
	r := setupRouter(auth.Bearer(auth.BearerConfig{
		Validator: func(token string) (string, bool) { return "user", true },
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/protected", nil)
	req.Header.Set("Authorization", "Bearer ")
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401 for empty token, got %d", rec.Code)
	}
}

func TestBearer_WrongScheme(t *testing.T) {
	r := setupRouter(auth.Bearer(auth.BearerConfig{
		Validator: func(token string) (string, bool) { return "user", true },
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/protected", nil)
	req.Header.Set("Authorization", "Basic dXNlcjpwYXNz")
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401 for Basic auth, got %d", rec.Code)
	}
}

// --- Helper Function Tests ---

func TestHelperFunctions(t *testing.T) {
	r := rig.New()

	r.GET("/test", func(c *rig.Context) error {
		// Test before authentication
		if auth.IsAuthenticated(c) {
			t.Error("should not be authenticated before setting context")
		}
		if auth.GetIdentity(c) != "" {
			t.Error("identity should be empty before authentication")
		}
		if auth.GetMethod(c) != "" {
			t.Error("method should be empty before authentication")
		}

		// Simulate authentication
		c.Set(auth.ContextKeyIdentity, "test-user")
		c.Set(auth.ContextKeyMethod, "api_key")

		// Test after authentication
		if !auth.IsAuthenticated(c) {
			t.Error("should be authenticated after setting context")
		}
		if auth.GetIdentity(c) != "test-user" {
			t.Errorf("expected identity 'test-user', got %s", auth.GetIdentity(c))
		}
		if auth.GetMethod(c) != "api_key" {
			t.Errorf("expected method 'api_key', got %s", auth.GetMethod(c))
		}

		return c.JSON(http.StatusOK, nil)
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
}

// Helper function for string contains check
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsAt(s, substr))
}

func containsAt(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
