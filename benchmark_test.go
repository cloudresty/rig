package rig

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
)

// ============================================================================
// Router Benchmarks
// ============================================================================

func BenchmarkRouter_StaticRoute(b *testing.B) {
	r := New()
	r.GET("/health", func(c *Context) error {
		c.Status(http.StatusOK)
		_, _ = c.WriteString("OK")
		return nil
	})

	req := httptest.NewRequest(http.MethodGet, "/health", nil)

	b.ReportAllocs()
	for b.Loop() {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
	}
}

func BenchmarkRouter_ParamRoute(b *testing.B) {
	r := New()
	r.GET("/users/{id}", func(c *Context) error {
		_ = c.Param("id")
		c.Status(http.StatusOK)
		_, _ = c.WriteString("OK")
		return nil
	})

	req := httptest.NewRequest(http.MethodGet, "/users/12345", nil)

	b.ReportAllocs()
	for b.Loop() {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
	}
}

func BenchmarkRouter_MultiParam(b *testing.B) {
	r := New()
	r.GET("/users/{userId}/posts/{postId}/comments/{commentId}", func(c *Context) error {
		_ = c.Param("userId")
		_ = c.Param("postId")
		_ = c.Param("commentId")
		c.Status(http.StatusOK)
		_, _ = c.WriteString("OK")
		return nil
	})

	req := httptest.NewRequest(http.MethodGet, "/users/1/posts/2/comments/3", nil)

	b.ReportAllocs()
	for b.Loop() {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
	}
}

// ============================================================================
// Context Benchmarks
// ============================================================================

func BenchmarkContext_JSON_Small(b *testing.B) {
	r := New()
	r.GET("/", func(c *Context) error {
		return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)

	b.ReportAllocs()
	for b.Loop() {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
	}
}

func BenchmarkContext_JSON_Large(b *testing.B) {
	type User struct {
		ID    int    `json:"id"`
		Name  string `json:"name"`
		Email string `json:"email"`
	}

	users := make([]User, 100)
	for i := range 100 {
		users[i] = User{ID: i, Name: "User Name", Email: "user@example.com"}
	}

	r := New()
	r.GET("/", func(c *Context) error {
		return c.JSON(http.StatusOK, users)
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)

	b.ReportAllocs()
	for b.Loop() {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
	}
}

func BenchmarkContext_Bind(b *testing.B) {
	type Input struct {
		Name  string `json:"name"`
		Email string `json:"email"`
	}

	body := []byte(`{"name":"John","email":"john@example.com"}`)

	r := New()
	r.POST("/", func(c *Context) error {
		var input Input
		if err := c.Bind(&input); err != nil {
			return err
		}
		c.Status(http.StatusOK)
		_, _ = c.WriteString("OK")
		return nil
	})

	b.ReportAllocs()
	for b.Loop() {
		req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
	}
}

func BenchmarkContext_Query(b *testing.B) {
	r := New()
	r.GET("/", func(c *Context) error {
		_ = c.Query("name")
		_ = c.Query("page")
		_ = c.Query("limit")
		c.Status(http.StatusOK)
		_, _ = c.WriteString("OK")
		return nil
	})

	req := httptest.NewRequest(http.MethodGet, "/?name=john&page=1&limit=10", nil)

	b.ReportAllocs()
	for b.Loop() {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
	}
}

func BenchmarkContext_SetGet(b *testing.B) {
	r := New()
	r.GET("/", func(c *Context) error {
		c.Set("user_id", 12345)
		c.Set("role", "admin")
		_, _ = c.Get("user_id")
		_, _ = c.Get("role")
		c.Status(http.StatusOK)
		_, _ = c.WriteString("OK")
		return nil
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)

	b.ReportAllocs()
	for b.Loop() {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
	}
}

// ============================================================================
// Middleware Benchmarks
// ============================================================================

func BenchmarkMiddleware_None(b *testing.B) {
	r := New()
	r.GET("/", func(c *Context) error {
		c.Status(http.StatusOK)
		_, _ = c.WriteString("OK")
		return nil
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)

	b.ReportAllocs()
	for b.Loop() {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
	}
}

func BenchmarkMiddleware_Single(b *testing.B) {
	r := New()
	r.Use(func(next HandlerFunc) HandlerFunc {
		return func(c *Context) error {
			c.Set("key", "value")
			return next(c)
		}
	})
	r.GET("/", func(c *Context) error {
		c.Status(http.StatusOK)
		_, _ = c.WriteString("OK")
		return nil
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)

	b.ReportAllocs()
	for b.Loop() {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
	}
}

func BenchmarkMiddleware_Chain5(b *testing.B) {
	r := New()
	for range 5 {
		r.Use(func(next HandlerFunc) HandlerFunc {
			return func(c *Context) error {
				return next(c)
			}
		})
	}
	r.GET("/", func(c *Context) error {
		c.Status(http.StatusOK)
		_, _ = c.WriteString("OK")
		return nil
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)

	b.ReportAllocs()
	for b.Loop() {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
	}
}

func BenchmarkMiddleware_Recover(b *testing.B) {
	r := New()
	r.Use(Recover())
	r.GET("/", func(c *Context) error {
		c.Status(http.StatusOK)
		_, _ = c.WriteString("OK")
		return nil
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)

	b.ReportAllocs()
	for b.Loop() {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
	}
}

func BenchmarkMiddleware_CORS(b *testing.B) {
	r := New()
	r.Use(DefaultCORS())
	r.GET("/", func(c *Context) error {
		c.Status(http.StatusOK)
		_, _ = c.WriteString("OK")
		return nil
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Origin", "http://example.com")

	b.ReportAllocs()
	for b.Loop() {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
	}
}

// ============================================================================
// Health Check Benchmarks
// ============================================================================

func BenchmarkHealth_SingleCheck(b *testing.B) {
	h := NewHealth()
	h.AddReadinessCheck("db", func() error { return nil })

	r := New()
	r.GET("/ready", h.ReadyHandler())

	req := httptest.NewRequest(http.MethodGet, "/ready", nil)

	b.ReportAllocs()
	for b.Loop() {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
	}
}

func BenchmarkHealth_MultipleChecks(b *testing.B) {
	h := NewHealth()
	h.AddReadinessCheck("database", func() error { return nil })
	h.AddReadinessCheck("cache", func() error { return nil })
	h.AddReadinessCheck("queue", func() error { return nil })

	r := New()
	r.GET("/ready", h.ReadyHandler())

	req := httptest.NewRequest(http.MethodGet, "/ready", nil)

	b.ReportAllocs()
	for b.Loop() {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
	}
}

// ============================================================================
// RouteGroup Benchmarks
// ============================================================================

func BenchmarkRouteGroup_Nested(b *testing.B) {
	r := New()
	api := r.Group("/api")
	v1 := api.Group("/v1")
	v1.GET("/users", func(c *Context) error {
		c.Status(http.StatusOK)
		_, _ = c.WriteString("OK")
		return nil
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/users", nil)

	b.ReportAllocs()
	for b.Loop() {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
	}
}

func BenchmarkRouteGroup_WithMiddleware(b *testing.B) {
	r := New()
	api := r.Group("/api")
	api.Use(func(next HandlerFunc) HandlerFunc {
		return func(c *Context) error {
			c.Set("version", "v1")
			return next(c)
		}
	})
	api.GET("/users", func(c *Context) error {
		c.Status(http.StatusOK)
		_, _ = c.WriteString("OK")
		return nil
	})

	req := httptest.NewRequest(http.MethodGet, "/api/users", nil)

	b.ReportAllocs()
	for b.Loop() {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
	}
}

// ============================================================================
// Parallel Benchmarks
// ============================================================================

// BenchmarkParallel_StaticRoute measures concurrent request handling.
func BenchmarkParallel_StaticRoute(b *testing.B) {
	r := New()
	r.GET("/health", func(c *Context) error {
		return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
	})

	b.ResetTimer()
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			req := httptest.NewRequest(http.MethodGet, "/health", nil)
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)
		}
	})
}
