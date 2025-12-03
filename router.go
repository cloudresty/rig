package rig

import (
	"net/http"
)

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

// ServeHTTP implements the http.Handler interface.
// This allows the Router to be used directly with http.ListenAndServe.
func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	r.mux.ServeHTTP(w, req)
}

// Handler returns the underlying http.ServeMux as an http.Handler.
func (r *Router) Handler() http.Handler {
	return r.mux
}

// Run starts the HTTP server on the given address.
// This is a convenience method that wraps http.ListenAndServe.
func (r *Router) Run(addr string) error {
	return http.ListenAndServe(addr, r)
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
	if prefix == "" {
		return path
	}
	if path == "" {
		return prefix
	}
	// Avoid double slash: "/api/" + "/users" -> "/api/users"
	if prefix[len(prefix)-1] == '/' && path[0] == '/' {
		return prefix + path[1:]
	}
	// Add missing slash: "/api" + "users" -> "/api/users"
	if prefix[len(prefix)-1] != '/' && path[0] != '/' {
		return prefix + "/" + path
	}
	return prefix + path
}
