// Package rig provides a thin, zero-dependency wrapper around the Go standard
// library's net/http package. It offers ergonomic benefits similar to frameworks
// like Gin or Echo while relying purely on the Go standard library.
package rig

// HandlerFunc is the custom handler signature for rig handlers.
// Unlike http.HandlerFunc, it accepts a *Context and returns an error,
// allowing handlers to return errors for centralized error handling.
type HandlerFunc func(*Context) error

// MiddlewareFunc is a function that wraps a HandlerFunc to provide
// additional functionality (logging, authentication, dependency injection, etc.).
// It follows the standard decorator pattern: it takes a handler and returns
// a new handler that wraps the original.
type MiddlewareFunc func(HandlerFunc) HandlerFunc

// ErrorHandler is a function type for handling errors returned by handlers.
// It receives the Context and the error, allowing custom error responses.
type ErrorHandler func(*Context, error)

// DefaultErrorHandler is the default error handler that writes a 500 Internal
// Server Error response when a handler returns an error.
func DefaultErrorHandler(c *Context, err error) {
	if err != nil {
		c.writer.WriteHeader(500)
		c.writer.Write([]byte("Internal Server Error"))
	}
}

