package rig

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestNew(t *testing.T) {
	r := New()

	if r == nil {
		t.Fatal("New() returned nil")
	}
	if r.mux == nil {
		t.Error("New() mux is nil")
	}
	if r.errorHandler == nil {
		t.Error("New() errorHandler is nil")
	}
}

func TestRouter_HTTPMethods(t *testing.T) {
	tests := []struct {
		method     string
		register   func(r *Router, path string, h HandlerFunc)
		wantStatus int
	}{
		{http.MethodGet, (*Router).GET, http.StatusOK},
		{http.MethodPost, (*Router).POST, http.StatusOK},
		{http.MethodPut, (*Router).PUT, http.StatusOK},
		{http.MethodDelete, (*Router).DELETE, http.StatusOK},
		{http.MethodPatch, (*Router).PATCH, http.StatusOK},
		{http.MethodOptions, (*Router).OPTIONS, http.StatusOK},
		{http.MethodHead, (*Router).HEAD, http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.method, func(t *testing.T) {
			r := New()
			called := false

			tt.register(r, "/test", func(c *Context) error {
				called = true
				return c.JSON(http.StatusOK, map[string]string{"method": tt.method})
			})

			req := httptest.NewRequest(tt.method, "/test", nil)
			w := httptest.NewRecorder()

			r.ServeHTTP(w, req)

			if !called {
				t.Error("handler was not called")
			}
			if w.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", w.Code, tt.wantStatus)
			}
		})
	}
}

func TestRouter_Handle(t *testing.T) {
	r := New()
	called := false

	r.Handle("GET /custom", func(c *Context) error {
		called = true
		return nil
	})

	req := httptest.NewRequest(http.MethodGet, "/custom", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if !called {
		t.Error("handler was not called")
	}
}

func TestRouter_PathParams(t *testing.T) {
	r := New()
	var capturedID string

	r.GET("/users/{id}", func(c *Context) error {
		capturedID = c.Param("id")
		return c.JSON(http.StatusOK, nil)
	})

	req := httptest.NewRequest(http.MethodGet, "/users/123", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if capturedID != "123" {
		t.Errorf("Param(id) = %q, want %q", capturedID, "123")
	}
}

func TestRouter_MultiplePathParams(t *testing.T) {
	r := New()
	var org, repo string

	r.GET("/orgs/{org}/repos/{repo}", func(c *Context) error {
		org = c.Param("org")
		repo = c.Param("repo")
		return nil
	})

	req := httptest.NewRequest(http.MethodGet, "/orgs/acme/repos/widget", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if org != "acme" {
		t.Errorf("Param(org) = %q, want %q", org, "acme")
	}
	if repo != "widget" {
		t.Errorf("Param(repo) = %q, want %q", repo, "widget")
	}
}

func TestRouter_ErrorHandler(t *testing.T) {
	r := New()
	testErr := errors.New("test error")

	r.GET("/error", func(c *Context) error {
		return testErr
	})

	req := httptest.NewRequest(http.MethodGet, "/error", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", w.Code, http.StatusInternalServerError)
	}
	if !strings.Contains(w.Body.String(), "Internal Server Error") {
		t.Errorf("body = %q, want to contain 'Internal Server Error'", w.Body.String())
	}
}

func TestRouter_CustomErrorHandler(t *testing.T) {
	r := New()
	testErr := errors.New("custom error")

	r.SetErrorHandler(func(c *Context, err error) {
		_ = c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	})

	r.GET("/error", func(c *Context) error {
		return testErr
	})

	req := httptest.NewRequest(http.MethodGet, "/error", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
	if !strings.Contains(w.Body.String(), "custom error") {
		t.Errorf("body = %q, want to contain 'custom error'", w.Body.String())
	}
}

func TestRouter_ErrorNotCalledWhenWritten(t *testing.T) {
	r := New()
	errorHandlerCalled := false

	r.SetErrorHandler(func(c *Context, err error) {
		errorHandlerCalled = true
	})

	r.GET("/error", func(c *Context) error {
		_ = c.JSON(http.StatusOK, map[string]string{"status": "ok"})
		return errors.New("this error should be ignored")
	})

	req := httptest.NewRequest(http.MethodGet, "/error", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if errorHandlerCalled {
		t.Error("error handler was called even though response was already written")
	}
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestRouter_Group(t *testing.T) {
	r := New()
	called := false

	api := r.Group("/api")
	api.GET("/users", func(c *Context) error {
		called = true
		return c.JSON(http.StatusOK, nil)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/users", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if !called {
		t.Error("grouped handler was not called")
	}
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestRouter_GroupNested(t *testing.T) {
	r := New()
	called := false

	api := r.Group("/api")
	v1 := api.Group("/v1")
	v1.GET("/users", func(c *Context) error {
		called = true
		return nil
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/users", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if !called {
		t.Error("nested grouped handler was not called")
	}
}

func TestRouteGroup_AllMethods(t *testing.T) {
	tests := []struct {
		method   string
		register func(g *RouteGroup, path string, h HandlerFunc)
	}{
		{http.MethodGet, (*RouteGroup).GET},
		{http.MethodPost, (*RouteGroup).POST},
		{http.MethodPut, (*RouteGroup).PUT},
		{http.MethodDelete, (*RouteGroup).DELETE},
		{http.MethodPatch, (*RouteGroup).PATCH},
	}

	for _, tt := range tests {
		t.Run(tt.method, func(t *testing.T) {
			r := New()
			g := r.Group("/api")
			called := false

			tt.register(g, "/test", func(c *Context) error {
				called = true
				return nil
			})

			req := httptest.NewRequest(tt.method, "/api/test", nil)
			w := httptest.NewRecorder()

			r.ServeHTTP(w, req)

			if !called {
				t.Errorf("%s handler was not called", tt.method)
			}
		})
	}
}

func TestRouter_Handler(t *testing.T) {
	r := New()

	r.GET("/test", func(c *Context) error {
		return c.JSON(http.StatusOK, nil)
	})

	handler := r.Handler()
	if handler == nil {
		t.Fatal("Handler() returned nil")
	}

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestRouter_ServeHTTP(t *testing.T) {
	r := New()
	called := false

	r.GET("/", func(c *Context) error {
		called = true
		return nil
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if !called {
		t.Error("ServeHTTP did not route to handler")
	}
}

func TestRouter_NotFound(t *testing.T) {
	r := New()

	r.GET("/exists", func(c *Context) error {
		return nil
	})

	req := httptest.NewRequest(http.MethodGet, "/notfound", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestRouter_MethodNotAllowed(t *testing.T) {
	r := New()

	r.GET("/resource", func(c *Context) error {
		return nil
	})

	req := httptest.NewRequest(http.MethodPost, "/resource", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed && w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 405 or 404", w.Code)
	}
}

func TestRouter_JSONRequest(t *testing.T) {
	r := New()

	type Input struct {
		Name string `json:"name"`
	}

	r.POST("/echo", func(c *Context) error {
		var input Input
		if err := c.Bind(&input); err != nil {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
		}
		return c.JSON(http.StatusOK, input)
	})

	body := strings.NewReader(`{"name":"test"}`)
	req := httptest.NewRequest(http.MethodPost, "/echo", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	want := `{"name":"test"}`
	got := strings.TrimSpace(w.Body.String())
	if got != want {
		t.Errorf("body = %q, want %q", got, want)
	}
}

func TestRouter_Middleware(t *testing.T) {
	r := New()
	var order []string

	// Add middleware that tracks execution order
	r.Use(func(next HandlerFunc) HandlerFunc {
		return func(c *Context) error {
			order = append(order, "mw1-before")
			err := next(c)
			order = append(order, "mw1-after")
			return err
		}
	})

	r.Use(func(next HandlerFunc) HandlerFunc {
		return func(c *Context) error {
			order = append(order, "mw2-before")
			err := next(c)
			order = append(order, "mw2-after")
			return err
		}
	})

	r.GET("/test", func(c *Context) error {
		order = append(order, "handler")
		return c.JSON(http.StatusOK, nil)
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// Verify middleware execution order
	expected := []string{"mw1-before", "mw2-before", "handler", "mw2-after", "mw1-after"}
	if len(order) != len(expected) {
		t.Fatalf("order length = %d, want %d", len(order), len(expected))
	}
	for i, v := range expected {
		if order[i] != v {
			t.Errorf("order[%d] = %q, want %q", i, order[i], v)
		}
	}
}

func TestRouter_MiddlewareWithContext(t *testing.T) {
	r := New()

	// Middleware that injects a value into context
	r.Use(func(next HandlerFunc) HandlerFunc {
		return func(c *Context) error {
			c.Set("user", "john")
			return next(c)
		}
	})

	r.GET("/test", func(c *Context) error {
		user := c.MustGet("user").(string)
		return c.JSON(http.StatusOK, map[string]string{"user": user})
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	want := `{"user":"john"}`
	got := strings.TrimSpace(w.Body.String())
	if got != want {
		t.Errorf("body = %q, want %q", got, want)
	}
}

func TestRouter_MiddlewareCanShortCircuit(t *testing.T) {
	r := New()
	handlerCalled := false

	// Middleware that short-circuits (doesn't call next)
	r.Use(func(next HandlerFunc) HandlerFunc {
		return func(c *Context) error {
			return c.JSON(http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		}
	})

	r.GET("/test", func(c *Context) error {
		handlerCalled = true
		return c.JSON(http.StatusOK, nil)
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if handlerCalled {
		t.Error("handler should not be called when middleware short-circuits")
	}
	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestRouter_MiddlewareErrorPropagation(t *testing.T) {
	r := New()
	errorHandlerCalled := false

	r.SetErrorHandler(func(c *Context, err error) {
		errorHandlerCalled = true
		_ = c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	})

	// Middleware that returns an error
	r.Use(func(next HandlerFunc) HandlerFunc {
		return func(c *Context) error {
			return errors.New("middleware error")
		}
	})

	r.GET("/test", func(c *Context) error {
		return c.JSON(http.StatusOK, nil)
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if !errorHandlerCalled {
		t.Error("error handler should be called when middleware returns error")
	}
}

func TestRouteGroup_Middleware(t *testing.T) {
	r := New()
	var order []string

	// Global middleware
	r.Use(func(next HandlerFunc) HandlerFunc {
		return func(c *Context) error {
			order = append(order, "global")
			return next(c)
		}
	})

	// Create group with its own middleware
	api := r.Group("/api")
	api.Use(func(next HandlerFunc) HandlerFunc {
		return func(c *Context) error {
			order = append(order, "group")
			return next(c)
		}
	})

	api.GET("/test", func(c *Context) error {
		order = append(order, "handler")
		return c.JSON(http.StatusOK, nil)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// Verify middleware execution order: global -> group -> handler
	expected := []string{"global", "group", "handler"}
	if len(order) != len(expected) {
		t.Fatalf("order length = %d, want %d; order = %v", len(order), len(expected), order)
	}
	for i, v := range expected {
		if order[i] != v {
			t.Errorf("order[%d] = %q, want %q", i, order[i], v)
		}
	}
}

func TestRouteGroup_NestedMiddleware(t *testing.T) {
	r := New()
	var order []string

	// Create nested groups with middleware
	api := r.Group("/api")
	api.Use(func(next HandlerFunc) HandlerFunc {
		return func(c *Context) error {
			order = append(order, "api")
			return next(c)
		}
	})

	v1 := api.Group("/v1")
	v1.Use(func(next HandlerFunc) HandlerFunc {
		return func(c *Context) error {
			order = append(order, "v1")
			return next(c)
		}
	})

	v1.GET("/test", func(c *Context) error {
		order = append(order, "handler")
		return c.JSON(http.StatusOK, nil)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/test", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// Verify middleware execution order: api -> v1 -> handler
	expected := []string{"api", "v1", "handler"}
	if len(order) != len(expected) {
		t.Fatalf("order length = %d, want %d; order = %v", len(order), len(expected), order)
	}
	for i, v := range expected {
		if order[i] != v {
			t.Errorf("order[%d] = %q, want %q", i, order[i], v)
		}
	}
}

func TestRouter_MiddlewareNotAffectOtherRoutes(t *testing.T) {
	r := New()
	middlewareCalled := false

	// Register route without middleware first
	r.GET("/no-middleware", func(c *Context) error {
		return c.JSON(http.StatusOK, map[string]string{"middleware": "no"})
	})

	// Then add middleware
	r.Use(func(next HandlerFunc) HandlerFunc {
		return func(c *Context) error {
			middlewareCalled = true
			return next(c)
		}
	})

	// Register route with middleware
	r.GET("/with-middleware", func(c *Context) error {
		return c.JSON(http.StatusOK, map[string]string{"middleware": "yes"})
	})

	// Test route registered before middleware
	req := httptest.NewRequest(http.MethodGet, "/no-middleware", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if middlewareCalled {
		t.Error("middleware should not be called for route registered before Use()")
	}

	// Test route registered after middleware
	middlewareCalled = false
	req = httptest.NewRequest(http.MethodGet, "/with-middleware", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if !middlewareCalled {
		t.Error("middleware should be called for route registered after Use()")
	}
}

func TestRouter_DependencyInjection(t *testing.T) {
	r := New()

	// Simulated database
	type Database struct {
		Name string
	}
	db := &Database{Name: "testdb"}

	// Middleware that injects database
	r.Use(func(next HandlerFunc) HandlerFunc {
		return func(c *Context) error {
			c.Set("db", db)
			return next(c)
		}
	})

	r.GET("/db-check", func(c *Context) error {
		database := c.MustGet("db").(*Database)
		return c.JSON(http.StatusOK, map[string]string{"db": database.Name})
	})

	req := httptest.NewRequest(http.MethodGet, "/db-check", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	want := `{"db":"testdb"}`
	got := strings.TrimSpace(w.Body.String())
	if got != want {
		t.Errorf("body = %q, want %q", got, want)
	}
}

func TestRouter_Use_MultipleMiddleware(t *testing.T) {
	r := New()
	var order []string

	// Add multiple middleware in one call
	r.Use(
		func(next HandlerFunc) HandlerFunc {
			return func(c *Context) error {
				order = append(order, "mw1")
				return next(c)
			}
		},
		func(next HandlerFunc) HandlerFunc {
			return func(c *Context) error {
				order = append(order, "mw2")
				return next(c)
			}
		},
		func(next HandlerFunc) HandlerFunc {
			return func(c *Context) error {
				order = append(order, "mw3")
				return next(c)
			}
		},
	)

	r.GET("/test", func(c *Context) error {
		order = append(order, "handler")
		return nil
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	expected := []string{"mw1", "mw2", "mw3", "handler"}
	if len(order) != len(expected) {
		t.Fatalf("order length = %d, want %d", len(order), len(expected))
	}
	for i, v := range expected {
		if order[i] != v {
			t.Errorf("order[%d] = %q, want %q", i, order[i], v)
		}
	}
}

func TestRouter_PathValidation(t *testing.T) {
	tests := []struct {
		name        string
		path        string
		shouldPanic bool
	}{
		{"valid path", "/users", false},
		{"valid path with param", "/users/{id}", false},
		{"root path", "/", false},
		{"empty path", "", true},
		{"no leading slash", "users", true},
		{"relative path", "users/", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := New()

			defer func() {
				rec := recover()
				if tt.shouldPanic && rec == nil {
					t.Errorf("expected panic for path %q", tt.path)
				}
				if !tt.shouldPanic && rec != nil {
					t.Errorf("unexpected panic for path %q: %v", tt.path, rec)
				}
			}()

			r.GET(tt.path, func(c *Context) error {
				return nil
			})
		})
	}
}

func TestRouter_PathValidation_AllMethods(t *testing.T) {
	methods := []struct {
		name     string
		register func(r *Router, path string, h HandlerFunc)
	}{
		{"GET", (*Router).GET},
		{"POST", (*Router).POST},
		{"PUT", (*Router).PUT},
		{"DELETE", (*Router).DELETE},
		{"PATCH", (*Router).PATCH},
		{"OPTIONS", (*Router).OPTIONS},
		{"HEAD", (*Router).HEAD},
	}

	for _, m := range methods {
		t.Run(m.name+"_invalid", func(t *testing.T) {
			r := New()

			defer func() {
				if recover() == nil {
					t.Errorf("%s should panic for empty path", m.name)
				}
			}()

			m.register(r, "", func(c *Context) error { return nil })
		})
	}
}

func TestRouter_GroupPathValidation(t *testing.T) {
	tests := []struct {
		name        string
		prefix      string
		shouldPanic bool
	}{
		{"valid prefix", "/api", false},
		{"valid nested prefix", "/api/v1", false},
		{"empty prefix", "", true},
		{"no leading slash", "api", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := New()

			defer func() {
				rec := recover()
				if tt.shouldPanic && rec == nil {
					t.Errorf("expected panic for prefix %q", tt.prefix)
				}
				if !tt.shouldPanic && rec != nil {
					t.Errorf("unexpected panic for prefix %q: %v", tt.prefix, rec)
				}
			}()

			r.Group(tt.prefix)
		})
	}
}

func TestRouteGroup_PathValidation(t *testing.T) {
	r := New()
	g := r.Group("/api")

	tests := []struct {
		name        string
		path        string
		shouldPanic bool
	}{
		{"empty path (group root)", "", false},
		{"valid path", "/users", false},
		{"no leading slash", "users", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				rec := recover()
				if tt.shouldPanic && rec == nil {
					t.Errorf("expected panic for path %q", tt.path)
				}
				if !tt.shouldPanic && rec != nil {
					t.Errorf("unexpected panic for path %q: %v", tt.path, rec)
				}
			}()

			g.GET(tt.path, func(c *Context) error { return nil })
		})
	}
}

func TestRouteGroup_NestedGroupPathValidation(t *testing.T) {
	r := New()
	api := r.Group("/api")

	// Valid nested group
	v1 := api.Group("/v1")
	if v1 == nil {
		t.Error("nested group should not be nil")
	}

	// Invalid nested group
	defer func() {
		if recover() == nil {
			t.Error("nested group with invalid prefix should panic")
		}
	}()

	api.Group("v2") // missing leading slash
}

func TestJoinPaths(t *testing.T) {
	tests := []struct {
		name   string
		prefix string
		path   string
		want   string
	}{
		{
			name:   "normal case",
			prefix: "/api",
			path:   "/users",
			want:   "/api/users",
		},
		{
			name:   "double slash prevention",
			prefix: "/api/",
			path:   "/users",
			want:   "/api/users",
		},
		{
			name:   "missing slash insertion",
			prefix: "/api",
			path:   "users",
			want:   "/api/users",
		},
		{
			name:   "empty prefix",
			prefix: "",
			path:   "/users",
			want:   "/users",
		},
		{
			name:   "empty path",
			prefix: "/api",
			path:   "",
			want:   "/api",
		},
		{
			name:   "both empty",
			prefix: "",
			path:   "",
			want:   "",
		},
		{
			name:   "trailing slash prefix only",
			prefix: "/api/",
			path:   "users",
			want:   "/api/users",
		},
		{
			name:   "nested groups double slash",
			prefix: "/api/v1/",
			path:   "/resources",
			want:   "/api/v1/resources",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := joinPaths(tt.prefix, tt.path)
			if got != tt.want {
				t.Errorf("joinPaths(%q, %q) = %q, want %q", tt.prefix, tt.path, got, tt.want)
			}
		})
	}
}

func TestRouter_GroupWithTrailingSlash(t *testing.T) {
	r := New()

	// Create group with trailing slash (user mistake)
	api := r.Group("/api/")

	// Add route with leading slash
	api.GET("/users", func(c *Context) error {
		return c.JSON(http.StatusOK, map[string]string{"path": "users"})
	})

	// Test that the route works without double slash
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/users", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestRouter_Static(t *testing.T) {
	// Create a temporary directory with a test file
	tmpDir, err := os.MkdirTemp("", "rig-static-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	// Create a test file
	testContent := "body { color: red; }"
	testFile := tmpDir + "/style.css"
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	r := New()
	r.Static("/assets", tmpDir)

	// Test serving the file
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/assets/style.css", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	if !strings.Contains(w.Body.String(), testContent) {
		t.Errorf("body = %q, should contain %q", w.Body.String(), testContent)
	}
}

func TestRouter_Static_WithTrailingSlash(t *testing.T) {
	// Create a temporary directory with a test file
	tmpDir, err := os.MkdirTemp("", "rig-static-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	// Create a test file
	testContent := "console.log('hello');"
	testFile := tmpDir + "/app.js"
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	r := New()
	// Path already has trailing slash
	r.Static("/static/", tmpDir)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/static/app.js", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	if !strings.Contains(w.Body.String(), testContent) {
		t.Errorf("body = %q, should contain %q", w.Body.String(), testContent)
	}
}

func TestRouter_Static_PathValidation(t *testing.T) {
	r := New()

	defer func() {
		if recover() == nil {
			t.Error("Static() should panic on invalid path")
		}
	}()

	r.Static("assets", ".") // missing leading slash
}

func TestRouter_Static_WithCacheControl(t *testing.T) {
	// Create a temporary directory with a test file
	tmpDir, err := os.MkdirTemp("", "rig-static-cache-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	// Create a test file
	testContent := "body { color: blue; }"
	testFile := tmpDir + "/style.css"
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	r := New()
	r.Static("/assets", tmpDir, StaticConfig{
		CacheControl: "public, max-age=31536000",
	})

	// Test serving the file with cache header
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/assets/style.css", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	cacheControl := w.Header().Get("Cache-Control")
	if cacheControl != "public, max-age=31536000" {
		t.Errorf("Cache-Control = %q, want %q", cacheControl, "public, max-age=31536000")
	}

	if !strings.Contains(w.Body.String(), testContent) {
		t.Errorf("body = %q, should contain %q", w.Body.String(), testContent)
	}
}

func TestRouter_Static_WithoutCacheControl(t *testing.T) {
	// Create a temporary directory with a test file
	tmpDir, err := os.MkdirTemp("", "rig-static-nocache-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	// Create a test file
	testContent := "console.log('test');"
	testFile := tmpDir + "/app.js"
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	r := New()
	// No config provided - should not set Cache-Control
	r.Static("/js", tmpDir)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/js/app.js", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	// Cache-Control should not be set when no config provided
	cacheControl := w.Header().Get("Cache-Control")
	if cacheControl != "" {
		t.Errorf("Cache-Control = %q, want empty (no config provided)", cacheControl)
	}
}

// --- Server Config Tests ---

func TestDefaultServerConfig(t *testing.T) {
	config := DefaultServerConfig()

	// ReadTimeout should be 0 (no limit) to allow slow uploads
	// Slowloris protection comes from ReadHeaderTimeout instead
	if config.ReadTimeout != 0 {
		t.Errorf("ReadTimeout = %v, want 0 (no body timeout)", config.ReadTimeout)
	}
	if config.WriteTimeout != 10*time.Second {
		t.Errorf("WriteTimeout = %v, want %v", config.WriteTimeout, 10*time.Second)
	}
	if config.IdleTimeout != 120*time.Second {
		t.Errorf("IdleTimeout = %v, want %v", config.IdleTimeout, 120*time.Second)
	}
	// ReadHeaderTimeout is the critical Slowloris defense
	if config.ReadHeaderTimeout != 5*time.Second {
		t.Errorf("ReadHeaderTimeout = %v, want %v", config.ReadHeaderTimeout, 5*time.Second)
	}
	if config.MaxHeaderBytes != 1<<20 {
		t.Errorf("MaxHeaderBytes = %v, want %v", config.MaxHeaderBytes, 1<<20)
	}
	if config.ShutdownTimeout != 5*time.Second {
		t.Errorf("ShutdownTimeout = %v, want %v", config.ShutdownTimeout, 5*time.Second)
	}
}

func TestServerConfig_CustomValues(t *testing.T) {
	customLogger := func(format string, args ...any) {}

	config := ServerConfig{
		Addr:              ":9090",
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
		ReadHeaderTimeout: 5 * time.Second,
		MaxHeaderBytes:    2 << 20,
		ShutdownTimeout:   10 * time.Second,
		Logger:            customLogger,
	}

	if config.Addr != ":9090" {
		t.Errorf("Addr = %v, want :9090", config.Addr)
	}
	if config.ReadTimeout != 15*time.Second {
		t.Errorf("ReadTimeout = %v, want %v", config.ReadTimeout, 15*time.Second)
	}
	if config.WriteTimeout != 30*time.Second {
		t.Errorf("WriteTimeout = %v, want %v", config.WriteTimeout, 30*time.Second)
	}
	if config.ShutdownTimeout != 10*time.Second {
		t.Errorf("ShutdownTimeout = %v, want %v", config.ShutdownTimeout, 10*time.Second)
	}
	if config.Logger == nil {
		t.Error("Logger should not be nil")
	}
}

func TestServerConfig_CustomLogger(t *testing.T) {
	var logs []string
	customLogger := func(format string, args ...any) {
		logs = append(logs, fmt.Sprintf(format, args...))
	}

	config := DefaultServerConfig()
	config.Logger = customLogger

	// Simulate what RunWithGracefulShutdown does with the logger
	logf := config.Logger
	logf("Rig server listening on %s", ":8080")
	logf("Shutdown signal received: %v", "interrupt")

	if len(logs) != 2 {
		t.Errorf("expected 2 log entries, got %d", len(logs))
	}
	if logs[0] != "Rig server listening on :8080" {
		t.Errorf("unexpected log: %s", logs[0])
	}
}

func TestServerConfig_SilentLogger(t *testing.T) {
	// Verify that a no-op logger can be used to silence output
	silentLogger := func(format string, args ...any) {}

	config := DefaultServerConfig()
	config.Logger = silentLogger

	// This should not panic
	config.Logger("test message %s", "value")
}

// --- Graceful Shutdown Tests ---

func TestRunGracefully_StartsAndShutdownsOnSignal(t *testing.T) {
	r := New()
	r.GET("/test", func(c *Context) error {
		return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
	})

	// Use a random available port
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to get free port: %v", err)
	}
	addr := listener.Addr().String()
	_ = listener.Close()

	// Track server start and shutdown
	serverStarted := make(chan struct{})
	serverDone := make(chan error, 1)

	go func() {
		// Override the default config to use our test address
		config := DefaultServerConfig()
		config.Addr = addr
		config.ShutdownTimeout = 1 * time.Second

		// We can't use RunGracefully directly since it blocks on signals,
		// so we'll test RunWithGracefulShutdown behavior by sending a signal
		close(serverStarted)
		serverDone <- r.RunWithGracefulShutdown(config)
	}()

	// Wait for server goroutine to start
	<-serverStarted
	time.Sleep(100 * time.Millisecond)

	// Make a request to verify server is running
	resp, err := http.Get("http://" + addr + "/test")
	if err != nil {
		t.Fatalf("server not responding: %v", err)
	}
	_ = resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	// Send SIGINT to trigger graceful shutdown
	// Note: We're sending to our own process which will be caught by the signal handler
	process, err := os.FindProcess(os.Getpid())
	if err != nil {
		t.Fatalf("failed to find process: %v", err)
	}

	// Send interrupt signal
	if err := process.Signal(syscall.SIGINT); err != nil {
		t.Fatalf("failed to send signal: %v", err)
	}

	// Wait for server to shut down
	select {
	case err := <-serverDone:
		if err != nil {
			t.Errorf("RunWithGracefulShutdown returned error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("server did not shut down within timeout")
	}
}

func TestRunWithGracefulShutdown_CompletesInFlightRequests(t *testing.T) {
	r := New()

	requestStarted := make(chan struct{})
	requestCanComplete := make(chan struct{})

	r.GET("/slow", func(c *Context) error {
		close(requestStarted)
		<-requestCanComplete
		return c.JSON(http.StatusOK, map[string]string{"status": "completed"})
	})

	// Get a free port
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to get free port: %v", err)
	}
	addr := listener.Addr().String()
	_ = listener.Close()

	serverDone := make(chan error, 1)

	go func() {
		config := DefaultServerConfig()
		config.Addr = addr
		config.ShutdownTimeout = 5 * time.Second
		serverDone <- r.RunWithGracefulShutdown(config)
	}()

	// Wait for server to start
	time.Sleep(100 * time.Millisecond)

	// Start a slow request
	responseChan := make(chan *http.Response, 1)
	go func() {
		resp, _ := http.Get("http://" + addr + "/slow")
		responseChan <- resp
	}()

	// Wait for request to start
	<-requestStarted

	// Send shutdown signal while request is in flight
	process, _ := os.FindProcess(os.Getpid())
	_ = process.Signal(syscall.SIGINT)

	// Allow the request to complete
	time.Sleep(100 * time.Millisecond)
	close(requestCanComplete)

	// Verify the request completed successfully
	select {
	case resp := <-responseChan:
		if resp != nil {
			if resp.StatusCode != http.StatusOK {
				t.Errorf("response status = %d, want %d", resp.StatusCode, http.StatusOK)
			}
			_ = resp.Body.Close()
		}
	case <-time.After(3 * time.Second):
		t.Error("request did not complete")
	}

	// Wait for server shutdown
	select {
	case <-serverDone:
		// Success
	case <-time.After(5 * time.Second):
		t.Fatal("server did not shut down")
	}
}
