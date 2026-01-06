package rig

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

// ServerConfig holds the configuration for the HTTP server.
// Use DefaultServerConfig() to get production-safe defaults that protect
// against Slowloris attacks and connection leaks.
type ServerConfig struct {
	// Addr is the TCP address to listen on (e.g., ":8080" or "127.0.0.1:8080").
	Addr string

	// ReadTimeout is the maximum duration for reading the entire request,
	// including the body. This prevents clients from holding connections
	// open indefinitely by sending data slowly.
	// Default: 5 seconds.
	ReadTimeout time.Duration

	// WriteTimeout is the maximum duration before timing out writes of the
	// response. This prevents slow clients from consuming server resources.
	// Default: 10 seconds.
	WriteTimeout time.Duration

	// IdleTimeout is the maximum amount of time to wait for the next request
	// when keep-alives are enabled. If zero, the value of ReadTimeout is used.
	// Default: 120 seconds.
	IdleTimeout time.Duration

	// ReadHeaderTimeout is the amount of time allowed to read request headers.
	// This is a critical defense against Slowloris attacks where attackers
	// send headers very slowly to exhaust server resources.
	// Default: 2 seconds.
	ReadHeaderTimeout time.Duration

	// MaxHeaderBytes controls the maximum number of bytes the server will
	// read parsing the request header's keys and values.
	// Default: 1MB (1 << 20).
	MaxHeaderBytes int

	// ShutdownTimeout is the maximum duration to wait for active connections
	// to finish during graceful shutdown. After this timeout, the server
	// forcefully closes remaining connections.
	// Only used by RunGracefully and RunWithGracefulShutdown.
	// Default: 5 seconds.
	ShutdownTimeout time.Duration
}

// DefaultServerConfig returns production-safe default timeouts.
// These settings protect against Slowloris attacks and ensure connections
// aren't held open indefinitely.
//
// The defaults are:
//   - ReadTimeout: 5s - prevents slow request body attacks
//   - WriteTimeout: 10s - prevents slow response consumption
//   - IdleTimeout: 120s - allows keep-alive but not indefinitely
//   - ReadHeaderTimeout: 2s - critical Slowloris protection
//   - MaxHeaderBytes: 1MB - prevents header size attacks
//   - ShutdownTimeout: 5s - time for graceful shutdown
func DefaultServerConfig() ServerConfig {
	return ServerConfig{
		ReadTimeout:       5 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       120 * time.Second,
		ReadHeaderTimeout: 2 * time.Second,
		MaxHeaderBytes:    1 << 20, // 1MB
		ShutdownTimeout:   5 * time.Second,
	}
}

// Router wraps http.ServeMux to provide a convenient API for routing
// HTTP requests with the custom HandlerFunc signature.
type Router struct {
	mux          *http.ServeMux
	errorHandler ErrorHandler
	middlewares  []MiddlewareFunc
}

// New creates a new Router with a fresh http.ServeMux.
func New() *Router {
	return &Router{
		mux:          http.NewServeMux(),
		errorHandler: DefaultErrorHandler,
		middlewares:  make([]MiddlewareFunc, 0),
	}
}

// SetErrorHandler sets a custom error handler for the router.
// This handler is called when a HandlerFunc returns an error.
func (r *Router) SetErrorHandler(handler ErrorHandler) {
	r.errorHandler = handler
}

// Use appends one or more middleware to the router's middleware stack.
// Middleware are executed in the order they are added.
func (r *Router) Use(mw ...MiddlewareFunc) {
	r.middlewares = append(r.middlewares, mw...)
}

// applyMiddleware wraps a handler with all registered middleware.
// Middleware are applied in reverse order so that the first registered
// middleware executes first (outermost wrapper).
func (r *Router) applyMiddleware(handler HandlerFunc) HandlerFunc {
	// Apply middleware in reverse order
	for i := len(r.middlewares) - 1; i >= 0; i-- {
		handler = r.middlewares[i](handler)
	}
	return handler
}

// wrap converts a rig.HandlerFunc into a standard http.HandlerFunc.
// It creates the Context and handles any errors returned by the handler.
func (r *Router) wrap(handler HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		ctx := newContext(w, req)

		if err := handler(ctx); err != nil {
			// Only call error handler if response hasn't been written
			if !ctx.Written() {
				r.errorHandler(ctx, err)
			}
		}
	}
}

// Handle registers a handler for the given pattern with any HTTP method.
// The pattern follows Go 1.22+ ServeMux patterns (e.g., "GET /users/{id}").
// The handler is wrapped with all registered middleware before being added.
func (r *Router) Handle(pattern string, handler HandlerFunc) {
	// Apply middleware chain to the handler
	wrapped := r.applyMiddleware(handler)
	r.mux.HandleFunc(pattern, r.wrap(wrapped))
}

// validatePath ensures the path is valid for Go 1.22+ ServeMux.
// It panics if the path is empty or doesn't start with '/'.
func validatePath(path string) {
	if path == "" || path[0] != '/' {
		panic("rig: path must begin with '/'")
	}
}

// GET registers a handler for GET requests at the given path.
// The path must begin with '/'. Panics if the path is invalid.
func (r *Router) GET(path string, handler HandlerFunc) {
	validatePath(path)
	r.Handle("GET "+path, handler)
}

// POST registers a handler for POST requests at the given path.
// The path must begin with '/'. Panics if the path is invalid.
func (r *Router) POST(path string, handler HandlerFunc) {
	validatePath(path)
	r.Handle("POST "+path, handler)
}

// PUT registers a handler for PUT requests at the given path.
// The path must begin with '/'. Panics if the path is invalid.
func (r *Router) PUT(path string, handler HandlerFunc) {
	validatePath(path)
	r.Handle("PUT "+path, handler)
}

// DELETE registers a handler for DELETE requests at the given path.
// The path must begin with '/'. Panics if the path is invalid.
func (r *Router) DELETE(path string, handler HandlerFunc) {
	validatePath(path)
	r.Handle("DELETE "+path, handler)
}

// PATCH registers a handler for PATCH requests at the given path.
// The path must begin with '/'. Panics if the path is invalid.
func (r *Router) PATCH(path string, handler HandlerFunc) {
	validatePath(path)
	r.Handle("PATCH "+path, handler)
}

// OPTIONS registers a handler for OPTIONS requests at the given path.
// The path must begin with '/'. Panics if the path is invalid.
func (r *Router) OPTIONS(path string, handler HandlerFunc) {
	validatePath(path)
	r.Handle("OPTIONS "+path, handler)
}

// HEAD registers a handler for HEAD requests at the given path.
// The path must begin with '/'. Panics if the path is invalid.
func (r *Router) HEAD(path string, handler HandlerFunc) {
	validatePath(path)
	r.Handle("HEAD "+path, handler)
}

// Static registers a route to serve static files from a directory.
// path is the URL path prefix (e.g., "/assets").
// root is the local file system directory (e.g., "./public").
//
// Example:
//
//	r.Static("/assets", "./public")
//	// GET /assets/css/style.css -> serves ./public/css/style.css
func (r *Router) Static(path, root string) {
	validatePath(path)

	// Ensure path ends with slash for correct StripPrefix behavior
	if path[len(path)-1] != '/' {
		path += "/"
	}

	// Create the file server handler
	fs := http.StripPrefix(path, http.FileServer(http.Dir(root)))

	// Wrap it in a Rig handler to support middleware
	handler := func(c *Context) error {
		fs.ServeHTTP(c.Writer(), c.Request())
		return nil
	}

	// Use Handle with trailing slash for Go 1.22+ wildcard matching
	// "GET /assets/" matches everything under it
	r.Handle("GET "+path, handler)
}

// ServeHTTP implements the http.Handler interface.
// This allows the Router to be used directly with http.ListenAndServe.
func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	r.mux.ServeHTTP(w, req)
}

// Handler returns the underlying http.ServeMux as an http.Handler.
func (r *Router) Handler() http.Handler {
	return r.mux
}

// Run starts the HTTP server on the given address with production-safe
// default timeouts. This protects against Slowloris attacks and connection leaks.
//
// For custom timeouts (e.g., long-polling endpoints), use RunWithConfig instead.
//
// Default timeouts applied:
//   - ReadTimeout: 5s
//   - WriteTimeout: 10s
//   - IdleTimeout: 120s
//   - ReadHeaderTimeout: 2s
//   - MaxHeaderBytes: 1MB
func (r *Router) Run(addr string) error {
	config := DefaultServerConfig()
	config.Addr = addr
	return r.RunWithConfig(config)
}

// RunWithConfig starts the HTTP server with specific configuration.
// This is the recommended method for production deployments where you need
// custom timeouts (e.g., long-polling, file uploads, streaming responses).
//
// Example:
//
//	config := rig.DefaultServerConfig()
//	config.Addr = ":8080"
//	config.WriteTimeout = 30 * time.Second // Allow longer responses
//	r.RunWithConfig(config)
func (r *Router) RunWithConfig(config ServerConfig) error {
	server := &http.Server{
		Addr:              config.Addr,
		Handler:           r,
		ReadTimeout:       config.ReadTimeout,
		WriteTimeout:      config.WriteTimeout,
		IdleTimeout:       config.IdleTimeout,
		ReadHeaderTimeout: config.ReadHeaderTimeout,
		MaxHeaderBytes:    config.MaxHeaderBytes,
	}
	return server.ListenAndServe()
}

// RunUnsafe starts the HTTP server without any timeouts.
// WARNING: This is only for development or testing. In production, this
// makes your server vulnerable to Slowloris attacks and connection leaks.
// Use Run() or RunWithConfig() instead.
func (r *Router) RunUnsafe(addr string) error {
	return http.ListenAndServe(addr, r)
}

// RunGracefully starts the HTTP server with production-safe defaults and
// waits for a termination signal (SIGINT or SIGTERM) to shut down gracefully.
//
// It ensures that active connections are given time to complete before the
// process exits. This is the recommended method for production deployments,
// especially in containerized environments (Docker, Kubernetes).
//
// The server will:
//  1. Listen for SIGINT (Ctrl+C) and SIGTERM (Docker stop, Kubernetes terminate)
//  2. Stop accepting new connections when a signal is received
//  3. Wait up to 5 seconds for active requests to complete
//  4. Forcefully close remaining connections after the timeout
//
// Example:
//
//	func main() {
//	    r := rig.New()
//	    r.GET("/", handler)
//	    if err := r.RunGracefully(":8080"); err != nil {
//	        log.Fatal(err)
//	    }
//	}
func (r *Router) RunGracefully(addr string) error {
	config := DefaultServerConfig()
	config.Addr = addr
	return r.RunWithGracefulShutdown(config)
}

// RunWithGracefulShutdown starts the server with specific configuration and
// handles graceful shutdown automatically. Use this when you need custom
// timeouts or shutdown behavior.
//
// Example:
//
//	config := rig.DefaultServerConfig()
//	config.Addr = ":8080"
//	config.WriteTimeout = 30 * time.Second  // Allow longer responses
//	config.ShutdownTimeout = 10 * time.Second  // More time for shutdown
//	r.RunWithGracefulShutdown(config)
func (r *Router) RunWithGracefulShutdown(config ServerConfig) error {
	server := &http.Server{
		Addr:              config.Addr,
		Handler:           r,
		ReadTimeout:       config.ReadTimeout,
		WriteTimeout:      config.WriteTimeout,
		IdleTimeout:       config.IdleTimeout,
		ReadHeaderTimeout: config.ReadHeaderTimeout,
		MaxHeaderBytes:    config.MaxHeaderBytes,
	}

	// Channel to listen for errors from the server
	serverErrors := make(chan error, 1)

	// Start the server in a goroutine so it doesn't block
	go func() {
		log.Printf("Rig server listening on %s", config.Addr)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErrors <- err
		}
	}()

	// Channel to listen for interrupt signals
	quit := make(chan os.Signal, 1)
	// SIGINT (Ctrl+C) and SIGTERM (Docker stop, Kubernetes terminate)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	// Block until we receive a signal or the server errors out
	select {
	case err := <-serverErrors:
		return fmt.Errorf("server error: %w", err)
	case sig := <-quit:
		log.Printf("Shutdown signal received: %v", sig)
	}

	// Use configured shutdown timeout, default to 5s if not set
	shutdownTimeout := config.ShutdownTimeout
	if shutdownTimeout == 0 {
		shutdownTimeout = 5 * time.Second
	}

	// Create a deadline for the shutdown
	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	log.Println("Shutting down server...")
	if err := server.Shutdown(ctx); err != nil {
		return fmt.Errorf("server forced to shutdown: %w", err)
	}

	log.Println("Server exited gracefully")
	return nil
}

// Group creates a new route group with the given prefix.
// All routes registered on the group will have the prefix prepended.
// The prefix must begin with '/'. Panics if the prefix is invalid.
func (r *Router) Group(prefix string) *RouteGroup {
	validatePath(prefix)
	return &RouteGroup{
		router:      r,
		prefix:      prefix,
		middlewares: make([]MiddlewareFunc, 0),
	}
}

// RouteGroup represents a group of routes with a common prefix.
// Groups can have their own middleware that applies only to routes in the group.
type RouteGroup struct {
	router      *Router
	prefix      string
	middlewares []MiddlewareFunc
}

// Use appends one or more middleware to the group's middleware stack.
// These middleware only apply to routes registered on this group.
func (g *RouteGroup) Use(mw ...MiddlewareFunc) {
	g.middlewares = append(g.middlewares, mw...)
}

// applyMiddleware wraps a handler with all group-specific middleware.
func (g *RouteGroup) applyMiddleware(handler HandlerFunc) HandlerFunc {
	for i := len(g.middlewares) - 1; i >= 0; i-- {
		handler = g.middlewares[i](handler)
	}
	return handler
}

// handle is an internal method that applies group middleware before
// delegating to the router's Handle method.
func (g *RouteGroup) handle(pattern string, handler HandlerFunc) {
	wrapped := g.applyMiddleware(handler)
	g.router.Handle(pattern, wrapped)
}

// validateGroupPath ensures the path is valid for a route group.
// Paths must either be empty (to match the group prefix exactly) or start with '/'.
func validateGroupPath(path string) {
	if path != "" && path[0] != '/' {
		panic("rig: path must be empty or begin with '/'")
	}
}

// GET registers a handler for GET requests at the given path within the group.
// The path must be empty or begin with '/'. Panics if the path is invalid.
func (g *RouteGroup) GET(path string, handler HandlerFunc) {
	validateGroupPath(path)
	g.handle("GET "+joinPaths(g.prefix, path), handler)
}

// POST registers a handler for POST requests at the given path within the group.
// The path must be empty or begin with '/'. Panics if the path is invalid.
func (g *RouteGroup) POST(path string, handler HandlerFunc) {
	validateGroupPath(path)
	g.handle("POST "+joinPaths(g.prefix, path), handler)
}

// PUT registers a handler for PUT requests at the given path within the group.
// The path must be empty or begin with '/'. Panics if the path is invalid.
func (g *RouteGroup) PUT(path string, handler HandlerFunc) {
	validateGroupPath(path)
	g.handle("PUT "+joinPaths(g.prefix, path), handler)
}

// DELETE registers a handler for DELETE requests at the given path within the group.
// The path must be empty or begin with '/'. Panics if the path is invalid.
func (g *RouteGroup) DELETE(path string, handler HandlerFunc) {
	validateGroupPath(path)
	g.handle("DELETE "+joinPaths(g.prefix, path), handler)
}

// PATCH registers a handler for PATCH requests at the given path within the group.
// The path must be empty or begin with '/'. Panics if the path is invalid.
func (g *RouteGroup) PATCH(path string, handler HandlerFunc) {
	validateGroupPath(path)
	g.handle("PATCH "+joinPaths(g.prefix, path), handler)
}

// Group creates a nested route group with an additional prefix.
// The nested group inherits the parent group's middleware.
// The prefix must begin with '/'. Panics if the prefix is invalid.
func (g *RouteGroup) Group(prefix string) *RouteGroup {
	validatePath(prefix)

	// Copy parent middleware to new group
	newMiddlewares := make([]MiddlewareFunc, len(g.middlewares))
	copy(newMiddlewares, g.middlewares)

	return &RouteGroup{
		router:      g.router,
		prefix:      joinPaths(g.prefix, prefix),
		middlewares: newMiddlewares,
	}
}

// joinPaths joins two URL path segments, handling edge cases with slashes.
// It prevents double slashes when prefix ends with '/' and path starts with '/'.
func joinPaths(prefix, path string) string {
	if path == "" {
		return prefix
	}

	finalPath := prefix
	if finalPath != "" && finalPath[len(finalPath)-1] == '/' {
		// Strip trailing slash from prefix if path has leading slash
		if path[0] == '/' {
			finalPath = finalPath[:len(finalPath)-1]
		}
	} else if finalPath != "" {
		// Add slash between prefix and path if path doesn't have one
		if path[0] != '/' {
			finalPath += "/"
		}
	}

	return finalPath + path
}
