package rig

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
)

// Context wraps http.ResponseWriter and *http.Request to provide
// convenient helper methods for HTTP handlers.
type Context struct {
	writer  http.ResponseWriter
	request *http.Request

	// written tracks whether the response has been written
	written bool

	// store is a key-value store for passing data between middleware and handlers.
	// It is lazily initialized to save memory on simple requests.
	store map[string]any
}

// newContext creates a new Context from the given ResponseWriter and Request.
func newContext(w http.ResponseWriter, r *http.Request) *Context {
	return &Context{
		writer:  w,
		request: r,
	}
}

// Request returns the underlying *http.Request.
func (c *Context) Request() *http.Request {
	return c.request
}

// Writer returns the underlying http.ResponseWriter.
func (c *Context) Writer() http.ResponseWriter {
	return c.writer
}

// Context returns the request's context.Context.
func (c *Context) Context() context.Context {
	return c.request.Context()
}

// JSON writes a JSON response with the given status code.
// It sets the Content-Type header to "application/json" and encodes
// the provided value v to the response body.
func (c *Context) JSON(code int, v any) error {
	c.writer.Header().Set("Content-Type", "application/json")
	c.writer.WriteHeader(code)
	c.written = true

	if v == nil {
		return nil
	}

	return json.NewEncoder(c.writer).Encode(v)
}

// Bind decodes the request body into the provided struct v.
// It expects the request body to be JSON and handles closing the body.
// The struct v should be a pointer.
func (c *Context) Bind(v any) error {
	if c.request.Body == nil {
		return nil
	}
	defer c.request.Body.Close()

	return json.NewDecoder(c.request.Body).Decode(v)
}

// Status writes the HTTP status code to the response.
// This should be called before writing any body content.
func (c *Context) Status(code int) {
	c.writer.WriteHeader(code)
	c.written = true
}

// Header returns the response header map.
func (c *Context) Header() http.Header {
	return c.writer.Header()
}

// SetHeader sets a response header with the given key and value.
func (c *Context) SetHeader(key, value string) {
	c.writer.Header().Set(key, value)
}

// GetHeader returns the value of the specified request header.
func (c *Context) GetHeader(key string) string {
	return c.request.Header.Get(key)
}

// Write writes data to the response body.
func (c *Context) Write(data []byte) (int, error) {
	c.written = true
	return c.writer.Write(data)
}

// WriteString writes a string to the response body.
func (c *Context) WriteString(s string) (int, error) {
	c.written = true
	return io.WriteString(c.writer, s)
}

// Param returns the value of a path parameter from the request.
// This uses Go 1.22+ PathValue feature.
func (c *Context) Param(name string) string {
	return c.request.PathValue(name)
}

// Query returns the value of a query string parameter.
func (c *Context) Query(key string) string {
	return c.request.URL.Query().Get(key)
}

// QueryDefault returns the value of a query string parameter,
// or the default value if the parameter is not present.
func (c *Context) QueryDefault(key, defaultValue string) string {
	value := c.request.URL.Query().Get(key)
	if value == "" {
		return defaultValue
	}
	return value
}

// Method returns the HTTP method of the request.
func (c *Context) Method() string {
	return c.request.Method
}

// Path returns the URL path of the request.
func (c *Context) Path() string {
	return c.request.URL.Path
}

// Written returns true if the response has been written.
func (c *Context) Written() bool {
	return c.written
}

// Set stores a value in the context's key-value store.
// The store is lazily initialized on first use to save memory.
func (c *Context) Set(key string, value any) {
	if c.store == nil {
		c.store = make(map[string]any)
	}
	c.store[key] = value
}

// Get retrieves a value from the context's key-value store.
// Returns the value and a boolean indicating whether the key was found.
func (c *Context) Get(key string) (any, bool) {
	if c.store == nil {
		return nil, false
	}
	value, exists := c.store[key]
	return value, exists
}

// MustGet retrieves a value from the context's key-value store.
// It panics if the key is not found. Use this only when you are certain
// the key exists (e.g., set by middleware earlier in the chain).
func (c *Context) MustGet(key string) any {
	value, exists := c.Get(key)
	if !exists {
		panic("rig: key '" + key + "' does not exist in context")
	}
	return value
}

