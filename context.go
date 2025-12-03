package rig

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
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

	// queryCache caches parsed query parameters to avoid re-parsing on each access.
	queryCache url.Values
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
// This is crucial for passing to database drivers and other libraries
// that listen for cancellation signals.
func (c *Context) Context() context.Context {
	return c.request.Context()
}

// SetContext updates the underlying request's context.
// Use this when middleware needs to set a timeout, deadline, or add
// tracing/telemetry data (e.g., OpenTelemetry spans) to the request.
//
// Example:
//
//	ctx, cancel := context.WithTimeout(c.Context(), 5*time.Second)
//	defer cancel()
//	c.SetContext(ctx)
func (c *Context) SetContext(ctx context.Context) {
	c.request = c.request.WithContext(ctx)
}

// JSON writes a JSON response with the given status code.
// It sets the Content-Type header to "application/json; charset=utf-8" and encodes
// the provided value v to the response body.
//
// Note: Headers and status code can only be written once. If you've already
// called Status(), Write(), or WriteString(), the headers set here will be ignored.
func (c *Context) JSON(code int, v any) error {
	if !c.written {
		c.writer.Header().Set("Content-Type", "application/json; charset=utf-8")
		c.writer.WriteHeader(code)
		c.written = true
	}

	if v == nil {
		return nil
	}

	return json.NewEncoder(c.writer).Encode(v)
}

// Bind decodes the request body into the provided struct v.
// It expects the request body to be JSON and handles closing the body.
// The struct v should be a pointer.
//
// By default, unknown fields in the JSON are silently ignored.
// For stricter APIs that should reject unknown fields, use BindStrict instead.
func (c *Context) Bind(v any) error {
	if c.request.Body == nil {
		return nil
	}
	defer func() { _ = c.request.Body.Close() }()

	return json.NewDecoder(c.request.Body).Decode(v)
}

// BindStrict decodes the request body into the provided struct v,
// but returns an error if the JSON contains fields that are not
// present in the target struct. This is useful for security-sensitive
// APIs where you want to reject unexpected data.
//
// Example error: "json: unknown field \"admin\""
func (c *Context) BindStrict(v any) error {
	if c.request.Body == nil {
		return nil
	}
	defer func() { _ = c.request.Body.Close() }()

	decoder := json.NewDecoder(c.request.Body)
	decoder.DisallowUnknownFields()
	return decoder.Decode(v)
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

// Redirect sends an HTTP redirect to the specified URL.
// The code should be a redirect status code (3xx), typically:
//   - http.StatusMovedPermanently (301) for permanent redirects
//   - http.StatusFound (302) for temporary redirects
//   - http.StatusSeeOther (303) for POST-to-GET redirects
//   - http.StatusTemporaryRedirect (307) to preserve method
//   - http.StatusPermanentRedirect (308) for permanent, method-preserving redirects
func (c *Context) Redirect(code int, url string) {
	http.Redirect(c.writer, c.request, url, code)
	c.written = true
}

// File writes the specified file into the response body.
// It uses http.ServeFile which handles Content-Type detection,
// range requests, and Last-Modified headers automatically.
//
// Example:
//
//	c.File("./reports/monthly.pdf")
func (c *Context) File(filepath string) {
	http.ServeFile(c.writer, c.request, filepath)
	c.written = true
}

// Data writes raw bytes to the response with the specified status code
// and content type.
//
// Example:
//
//	c.Data(http.StatusOK, "image/png", pngBytes)
func (c *Context) Data(code int, contentType string, data []byte) {
	c.writer.Header().Set("Content-Type", contentType)
	c.Status(code)
	_, _ = c.writer.Write(data)
}

// Param returns the value of a path parameter from the request.
// This uses Go 1.22+ PathValue feature.
func (c *Context) Param(name string) string {
	return c.request.PathValue(name)
}

// queryParams returns the cached query parameters, parsing them on first access.
func (c *Context) queryParams() url.Values {
	if c.queryCache == nil {
		c.queryCache = c.request.URL.Query()
	}
	return c.queryCache
}

// Query returns the value of a query string parameter.
// Query parameters are parsed once and cached for efficient repeated access.
func (c *Context) Query(key string) string {
	return c.queryParams().Get(key)
}

// QueryDefault returns the value of a query string parameter,
// or the default value if the parameter is not present or empty.
// Query parameters are parsed once and cached for efficient repeated access.
func (c *Context) QueryDefault(key, defaultValue string) string {
	value := c.queryParams().Get(key)
	if value == "" {
		return defaultValue
	}
	return value
}

// QueryArray returns all values for a query string parameter.
// This is useful for parameters that can have multiple values like ?tag=a&tag=b.
// Query parameters are parsed once and cached for efficient repeated access.
func (c *Context) QueryArray(key string) []string {
	return c.queryParams()[key]
}

// FormValue returns the first value for the named component of the query.
// POST and PUT body parameters take precedence over URL query string values.
// This is useful for handling HTML form submissions (application/x-www-form-urlencoded).
func (c *Context) FormValue(key string) string {
	return c.request.FormValue(key)
}

// PostFormValue returns the first value for the named component of the POST,
// PATCH, or PUT request body. URL query parameters are ignored.
// Use this when you want to explicitly read only from the request body.
func (c *Context) PostFormValue(key string) string {
	return c.request.PostFormValue(key)
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

// GetType retrieves a value from the context's key-value store and asserts
// its type safely using generics. This is the recommended way to retrieve
// typed values as it avoids panics from incorrect type assertions.
//
// Example:
//
//	db, err := rig.GetType[*Database](c, "db")
//	if err != nil {
//	    return err
//	}
//	db.Query(...)
func GetType[T any](c *Context, key string) (T, error) {
	var zero T

	value, ok := c.Get(key)
	if !ok {
		return zero, fmt.Errorf("rig: key '%s' not found in context", key)
	}

	typedValue, ok := value.(T)
	if !ok {
		return zero, fmt.Errorf("rig: value for key '%s' is not of type %T", key, zero)
	}

	return typedValue, nil
}
